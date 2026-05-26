package services

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

const (
	healthDialTimeout = 3 * time.Second
)

// NodeHealth хранит результат последней TCP-проверки узла.
type NodeHealth struct {
	Alive     bool      `json:"alive"`
	LatencyMs int64     `json:"latency_ms"` // -1 если недоступен
	Checked   time.Time `json:"checked"`
}

// SubscriptionHealthService выполняет TCP-проверки узлов подписок по запросу
// и кэширует результаты в оперативной памяти.
type SubscriptionHealthService struct {
	dataDir         string
	subscriptionSvc *SubscriptionService
	dialFunc        func(network, address string, timeout time.Duration) (net.Conn, error)

	mu    sync.RWMutex
	cache map[string]map[string]NodeHealth // subscriptionID -> tag -> NodeHealth
}

func NewSubscriptionHealthService(dataDir string, subscriptionSvc *SubscriptionService) *SubscriptionHealthService {
	return &SubscriptionHealthService{
		dataDir:         dataDir,
		subscriptionSvc: subscriptionSvc,
		cache:           make(map[string]map[string]NodeHealth),
	}
}

// Start запускает сервис (заглушка, так как фоновый опрос отключен).
func (s *SubscriptionHealthService) Start() {
	log.Println("[HealthSvc] Service started (on-demand mode)")
}

// Stop останавливает сервис (заглушка).
func (s *SubscriptionHealthService) Stop() {
	log.Println("[HealthSvc] Service stopped")
}

// GetHealth возвращает кэшированные результаты для подписки из оперативной памяти.
func (s *SubscriptionHealthService) GetHealth(subscriptionID string) map[string]NodeHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]NodeHealth)
	for tag, h := range s.cache[subscriptionID] {
		result[tag] = h
	}
	return result
}

// ForceCheck немедленно проверяет все узлы указанной подписки.
func (s *SubscriptionHealthService) ForceCheck(subscriptionID string) {
	sub := s.subscriptionSvc.Get(subscriptionID)
	if sub == nil {
		return
	}
	s.checkSubscription(sub)
}

// ForceCheckNode немедленно проверяет один конкретный узел указанной подписки.
func (s *SubscriptionHealthService) ForceCheckNode(subscriptionID, nodeTag string) (NodeHealth, bool) {
	sub := s.subscriptionSvc.Get(subscriptionID)
	if sub == nil {
		return NodeHealth{LatencyMs: -1, Checked: time.Now()}, false
	}
	var targetServer string
	for _, node := range sub.Nodes {
		if node.Tag == nodeTag {
			targetServer = node.Server
			break
		}
	}
	if targetServer == "" {
		return NodeHealth{LatencyMs: -1, Checked: time.Now()}, false
	}

	h := s.dialNode(targetServer)

	s.mu.Lock()
	if s.cache[subscriptionID] == nil {
		s.cache[subscriptionID] = make(map[string]NodeHealth)
	}
	s.cache[subscriptionID][nodeTag] = h
	s.mu.Unlock()

	return h, true
}

func (s *SubscriptionHealthService) checkSubscription(sub *Subscription) {
	if len(sub.Nodes) == 0 {
		return
	}

	results := make(map[string]NodeHealth, len(sub.Nodes))
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 10) // Ограничиваем параллельность 10 воркерами

	for _, node := range sub.Nodes {
		if node.Server == "" {
			continue
		}
		wg.Add(1)
		go func(tag, server string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			h := s.dialNode(server)
			mu.Lock()
			results[tag] = h
			mu.Unlock()
		}(node.Tag, node.Server)
	}
	wg.Wait()

	if len(results) == 0 {
		return
	}

	s.mu.Lock()
	s.cache[sub.ID] = results
	s.mu.Unlock()

	log.Printf("[HealthSvc] sub %s: checked %d nodes on-demand", sub.ID, len(results))
}

// dialNode выполняет TCP-dial к адресу host:port и возвращает NodeHealth.
func (s *SubscriptionHealthService) dialNode(server string) NodeHealth {
	start := time.Now()
	var conn net.Conn
	var err error

	if s.dialFunc != nil {
		conn, err = s.dialFunc("tcp", server, healthDialTimeout)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), healthDialTimeout)
		defer cancel()
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", server)
	}

	latency := time.Since(start).Milliseconds()
	if err != nil {
		return NodeHealth{Alive: false, LatencyMs: -1, Checked: time.Now()}
	}
	if conn != nil {
		conn.Close()
	}
	return NodeHealth{Alive: true, LatencyMs: latency, Checked: time.Now()}
}
