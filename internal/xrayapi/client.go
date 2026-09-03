package xrayapi

import (
	"context"
	"strings"

	"google.golang.org/grpc"

	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
)

// TrafficPair holds uplink and downlink byte counters for a traffic direction.
type TrafficPair struct {
	Uplink   int64 `json:"uplink"`
	Downlink int64 `json:"downlink"`
}

// Client wraps gRPC clients for Xray services.
type Client struct {
	conn  *grpc.ClientConn
	stats statspb.StatsServiceClient
}

// NewClient creates a new Client using an existing gRPC connection.
func NewClient(conn *grpc.ClientConn) *Client {
	return &Client{
		conn:  conn,
		stats: statspb.NewStatsServiceClient(conn),
	}
}

// OutboundTraffic queries all outbound traffic counters with pattern "outbound>>>".
// It parses counter names in format: outbound>>>{tag}>>>traffic>>>{direction}
// and returns a map of outbound tags to TrafficPair.
func (c *Client) OutboundTraffic(ctx context.Context) (map[string]TrafficPair, error) {
	resp, err := c.stats.QueryStats(ctx, &statspb.QueryStatsRequest{
		Pattern: "outbound>>>",
		Reset_:  false,
	})
	if err != nil {
		return nil, err
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
	return c.stats.GetSysStats(ctx, &statspb.SysStatsRequest{})
}
