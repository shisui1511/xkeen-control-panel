package xrayapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	logpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/log/command"
	routerpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/router/command"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	netpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/common/net"
)

var (
	// ErrCoreUnavailable indicates that Xray core gRPC endpoint is not reachable or timed out.
	ErrCoreUnavailable = errors.New("xray core is unavailable")

	// ErrInvalidArgument indicates invalid request arguments.
	ErrInvalidArgument = errors.New("invalid argument")
)

// normalizeGRPCError maps gRPC transport status codes to typed Go errors.
func normalizeGRPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return fmt.Errorf("%w: %s", ErrCoreUnavailable, st.Message())
		case codes.InvalidArgument:
			return fmt.Errorf("%w: %s", ErrInvalidArgument, st.Message())
		}
	}
	return err
}

// TrafficPair holds uplink and downlink byte counters for a traffic direction.
type TrafficPair struct {
	Uplink   int64 `json:"uplink"`
	Downlink int64 `json:"downlink"`
}

// RouteTestInput describes connection parameters to test against Xray routing rules.
type RouteTestInput struct {
	Domain     string            `json:"domain,omitempty"`
	IP         string            `json:"ip,omitempty"`
	Port       uint32            `json:"port,omitempty"`
	Network    string            `json:"network,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	InboundTag string            `json:"inbound_tag,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// RouteTestResult contains routing decision returned by Xray core.
type RouteTestResult struct {
	OutboundTag       string   `json:"outbound_tag"`
	OutboundGroupTags []string `json:"outbound_group_tags,omitempty"`
	RuleGroups        []string `json:"rule_groups,omitempty"`
	Matched           bool     `json:"matched"`
}

// Client wraps gRPC clients for Xray services sharing a single connection.
type Client struct {
	conn   *grpc.ClientConn
	stats  statspb.StatsServiceClient
	router routerpb.RoutingServiceClient
	logger logpb.LoggerServiceClient
}

// NewClient creates a new Client using an existing gRPC connection.
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{
		conn:   conn,
		stats:  statspb.NewStatsServiceClient(conn),
		router: routerpb.NewRoutingServiceClient(conn),
		logger: logpb.NewLoggerServiceClient(conn),
	}
}

// OutboundTraffic queries all outbound traffic counters with pattern "outbound>>>".
// It parses counter names in format: outbound>>>{tag}>>>traffic>>>{direction}
// and returns a map of outbound tags to TrafficPair.
func (c *Client) OutboundTraffic(ctx context.Context) (map[string]TrafficPair, error) {
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := c.stats.QueryStats(callCtx, &statspb.QueryStatsRequest{
		Pattern: "outbound>>>",
		Reset_:  false,
	})
	if err != nil {
		return nil, normalizeGRPCError(err)
	}

	result := make(map[string]TrafficPair)
	for _, stat := range resp.GetStat() {
		parts := strings.Split(stat.GetName(), ">>>")
		if len(parts) == 4 && parts[0] == "outbound" && parts[2] == "traffic" {
			tag := parts[1]
			dir := parts[3]
			pair := result[tag]
			if dir == "uplink" {
				pair.Uplink = stat.GetValue()
			} else if dir == "downlink" {
				pair.Downlink = stat.GetValue()
			}
			result[tag] = pair
		}
	}
	return result, nil
}

// SysStats retrieves runtime system stats from Xray.
func (c *Client) SysStats(ctx context.Context) (*statspb.SysStatsResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := c.stats.GetSysStats(callCtx, &statspb.SysStatsRequest{})
	if err != nil {
		return nil, normalizeGRPCError(err)
	}
	return resp, nil
}

// TestRoute asks Xray core which outbound rule matches the given connection parameters.
func (c *Client) TestRoute(ctx context.Context, input RouteTestInput) (*RouteTestResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var netType netpb.Network
	switch strings.ToLower(input.Network) {
	case "udp":
		netType = netpb.Network_UDP
	case "unix":
		netType = netpb.Network_UNIX
	default:
		netType = netpb.Network_TCP
	}

	var targetIPs [][]byte
	if input.IP != "" {
		if parsed := net.ParseIP(input.IP); parsed != nil {
			if v4 := parsed.To4(); v4 != nil {
				targetIPs = [][]byte{v4}
			} else {
				targetIPs = [][]byte{parsed.To16()}
			}
		}
	}

	port := input.Port
	if port == 0 {
		port = 443
	}

	req := &routerpb.TestRouteRequest{
		RoutingContext: &routerpb.RoutingContext{
			InboundTag:   input.InboundTag,
			Network:      netType,
			TargetDomain: input.Domain,
			TargetIPs:    targetIPs,
			TargetPort:   port,
			Protocol:     input.Protocol,
			Attributes:   input.Attributes,
		},
		PublishResult: false,
	}

	resp, err := c.router.TestRoute(callCtx, req)
	if err != nil {
		return nil, normalizeGRPCError(err)
	}

	outTag := resp.GetOutboundTag()
	groups := resp.GetOutboundGroupTags()
	return &RouteTestResult{
		OutboundTag:       outTag,
		OutboundGroupTags: groups,
		RuleGroups:        groups,
		Matched:           outTag != "",
	}, nil
}

// RestartLogger requests Xray core to reopen its log files for safe log rotation.
func (c *Client) RestartLogger(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err := c.logger.RestartLogger(callCtx, &logpb.RestartLoggerRequest{})
	if err != nil {
		return normalizeGRPCError(err)
	}
	return nil
}
