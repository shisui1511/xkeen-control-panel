package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ClientInfo содержит сведения о клиенте локальной сети (LAN).
type ClientInfo struct {
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Name        string `json:"name,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	DisplayName string `json:"display_name"`
	Active      bool   `json:"active"`
	Link        string `json:"link,omitempty"`
	Interface   string `json:"interface,omitempty"`
}

// ClientResolver разрешает IP-адреса клиентов в имена устройств и сетевые метаданные
// через опрос Keenetic RCI (`/rci/show/ip/hotspot`), fallback на `ndmc` и `/proc/net/arp`
// с кэшированием результатов.
type ClientResolver struct {
	mu        sync.RWMutex
	cache     map[string]ClientInfo
	lastFetch time.Time
	ttl       time.Duration

	rciURL     string
	httpClient *http.Client

	// Hook для мокирования вызова ndmc в тестах
	execNdmc func(ctx context.Context) ([]byte, error)
	// Hook для мокирования чтения arp в тестах
	readArp func() ([]byte, error)
}

// NewClientResolver создает новый экземпляр ClientResolver с TTL кэша 20 секунд.
func NewClientResolver() *ClientResolver {
	return &ClientResolver{
		ttl:    20 * time.Second,
		rciURL: "http://127.0.0.1:79/rci/show/ip/hotspot",
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
		execNdmc: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "ndmc", "-c", "show ip hotspot").Output()
		},
		readArp: func() ([]byte, error) {
			return os.ReadFile("/proc/net/arp")
		},
	}
}

// SetTTL настраивает время жизни кэша.
func (r *ClientResolver) SetTTL(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttl = d
}

// SetRCIURL переопределяет URL RCI (для тестов).
func (r *ClientResolver) SetRCIURL(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rciURL = url
}

// GetClients возвращает карту IP -> ClientInfo.
func (r *ClientResolver) GetClients() map[string]ClientInfo {
	r.mu.RLock()
	if r.cache != nil && time.Since(r.lastFetch) < r.ttl {
		copied := copyClientMap(r.cache)
		r.mu.RUnlock()
		return copied
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Двойная проверка после взятия write lock
	if r.cache != nil && time.Since(r.lastFetch) < r.ttl {
		return copyClientMap(r.cache)
	}

	clients := r.fetchClients()
	if len(clients) > 0 {
		r.cache = clients
		r.lastFetch = time.Now()
		return copyClientMap(r.cache)
	}

	// Если свежее получение не удалось, возвращаем устаревший кэш (если есть) или пустую карту
	if r.cache != nil {
		return copyClientMap(r.cache)
	}
	return make(map[string]ClientInfo)
}

// Resolve возвращает ClientInfo по конкретному IP адресу.
func (r *ClientResolver) Resolve(ip string) (ClientInfo, bool) {
	clients := r.GetClients()
	c, ok := clients[ip]
	return c, ok
}

func (r *ClientResolver) fetchClients() map[string]ClientInfo {
	// 1. Попытка через Keenetic RCI HTTP (порт 79)
	if clients, err := r.fetchFromRCI(); err == nil && len(clients) > 0 {
		return clients
	}

	// 2. Fallback через ndmc CLI
	if clients, err := r.fetchFromNdmc(); err == nil && len(clients) > 0 {
		return clients
	}

	// 3. Fallback через /proc/net/arp
	if clients, err := r.fetchFromArp(); err == nil && len(clients) > 0 {
		return clients
	}

	return nil
}

type rciHotspotResponse struct {
	Host []struct {
		MAC       string `json:"mac"`
		IP        string `json:"ip"`
		Hostname  string `json:"hostname"`
		Name      string `json:"name"`
		Active    bool   `json:"active"`
		Link      string `json:"link"`
		Interface struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"interface"`
	} `json:"host"`
}

func (r *ClientResolver) fetchFromRCI() (map[string]ClientInfo, error) {
	if r.rciURL == "" || r.httpClient == nil {
		return nil, fmt.Errorf("rci not configured")
	}

	req, err := http.NewRequest(http.MethodGet, r.rciURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rci returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return ParseRCIHotspotJSON(body)
}

// ParseRCIHotspotJSON парсит JSON-ответ Keenetic RCI hotspot.
func ParseRCIHotspotJSON(data []byte) (map[string]ClientInfo, error) {
	var resp rciHotspotResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	result := make(map[string]ClientInfo, len(resp.Host))
	for _, h := range resp.Host {
		ip := strings.TrimSpace(h.IP)
		if ip == "" || ip == "0.0.0.0" {
			continue
		}

		name := strings.TrimSpace(h.Name)
		hostname := strings.TrimSpace(h.Hostname)
		mac := strings.TrimSpace(strings.ToLower(h.MAC))
		displayName := name
		if displayName == "" {
			displayName = hostname
		}
		if displayName == "" {
			displayName = ip
		}

		result[ip] = ClientInfo{
			IP:          ip,
			MAC:         mac,
			Name:        name,
			Hostname:    hostname,
			DisplayName: displayName,
			Active:      h.Active,
			Link:        strings.TrimSpace(h.Link),
			Interface:   strings.TrimSpace(h.Interface.Name),
		}
	}
	return result, nil
}

func (r *ClientResolver) fetchFromNdmc() (map[string]ClientInfo, error) {
	if r.execNdmc == nil {
		return nil, fmt.Errorf("ndmc executor not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := r.execNdmc(ctx)
	if err != nil || len(out) == 0 {
		return nil, err
	}

	return ParseNdmcHotspotText(string(out)), nil
}

var (
	ndmcKeyValRe = regexp.MustCompile(`^\s*([a-zA-Z0-9_-]+):\s*(.*)$`)
)

// ParseNdmcHotspotText парсит текстовый вывод команды `ndmc -c "show ip hotspot"`.
func ParseNdmcHotspotText(text string) map[string]ClientInfo {
	result := make(map[string]ClientInfo)
	scanner := bufio.NewScanner(strings.NewReader(text))

	var current ClientInfo
	var inHost bool

	saveCurrent := func() {
		if current.IP != "" && current.IP != "0.0.0.0" {
			if current.DisplayName == "" {
				if current.Name != "" {
					current.DisplayName = current.Name
				} else if current.Hostname != "" {
					current.DisplayName = current.Hostname
				} else {
					current.DisplayName = current.IP
				}
			}
			result[current.IP] = current
		}
		current = ClientInfo{}
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "host:") {
			saveCurrent()
			inHost = true
			continue
		}

		matches := ndmcKeyValRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			key := strings.ToLower(strings.TrimSpace(matches[1]))
			val := strings.TrimSpace(matches[2])

			switch key {
			case "mac":
				if inHost && current.MAC == "" {
					current.MAC = strings.ToLower(val)
				}
			case "ip":
				if inHost && current.IP == "" {
					current.IP = val
				}
			case "hostname":
				if inHost && current.Hostname == "" {
					current.Hostname = val
				}
			case "name":
				if inHost && current.Name == "" {
					current.Name = val
				}
			case "active":
				if strings.EqualFold(val, "yes") || strings.EqualFold(val, "true") {
					current.Active = true
				}
			case "link":
				current.Link = val
			}
		}
	}
	saveCurrent()

	return result
}

func (r *ClientResolver) fetchFromArp() (map[string]ClientInfo, error) {
	if r.readArp == nil {
		return nil, fmt.Errorf("arp reader not set")
	}

	data, err := r.readArp()
	if err != nil || len(data) == 0 {
		return nil, err
	}

	return ParseArpTable(string(data)), nil
}

// ParseArpTable парсит данные /proc/net/arp.
func ParseArpTable(data string) map[string]ClientInfo {
	result := make(map[string]ClientInfo)
	scanner := bufio.NewScanner(strings.NewReader(data))

	// Пропускаем заголовок
	if scanner.Scan() {
		_ = scanner.Text()
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		ip := fields[0]
		mac := strings.ToLower(fields[3])

		// Игнорируем неполные / пустые ARP-записи
		if mac == "00:00:00:00:00:00" || ip == "" || ip == "0.0.0.0" {
			continue
		}

		result[ip] = ClientInfo{
			IP:          ip,
			MAC:         mac,
			DisplayName: ip,
			Active:      true,
		}
	}
	return result
}

func copyClientMap(m map[string]ClientInfo) map[string]ClientInfo {
	copied := make(map[string]ClientInfo, len(m))
	for k, v := range m {
		copied[k] = v
	}
	return copied
}
