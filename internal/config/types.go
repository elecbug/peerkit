package config

type Scenario struct {
	Version    int              `yaml:"version" json:"version"`
	Experiment ExperimentConfig `yaml:"experiment" json:"experiment"`
	Defaults   DefaultsConfig   `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Domain     *DomainConfig    `yaml:"domain,omitempty" json:"domain,omitempty"`
	Topology   TopologyConfig   `yaml:"topology,omitempty" json:"topology,omitempty"`
	Traffic    []TrafficConfig  `yaml:"traffic" json:"traffic"`
}

type ExperimentConfig struct {
	Name            string `yaml:"name" json:"name"`
	Seed            int64  `yaml:"seed" json:"seed"`
	DurationMS      int64  `yaml:"duration_ms" json:"duration_ms"`
	WarmupMS        int64  `yaml:"warmup_ms" json:"warmup_ms"`
	ControlBasePort int    `yaml:"control_base_port" json:"control_base_port"`
}

type DefaultsConfig struct {
	Node NodePerformance `yaml:"node" json:"node"`
	Edge EdgeNetwork     `yaml:"edge" json:"edge"`
}

// DomainConfig defines a generated experiment domain. It is expanded into the
// explicit TopologyConfig before validation and execution.
type DomainConfig struct {
	N           int                  `yaml:"n,omitempty" json:"n,omitempty"`
	NodeCount   int                  `yaml:"node_count,omitempty" json:"node_count,omitempty"`
	IDPrefix    string               `yaml:"id_prefix,omitempty" json:"id_prefix,omitempty"`
	ZeroPadding int                  `yaml:"zero_padding,omitempty" json:"zero_padding,omitempty"`
	Topology    DomainTopologyConfig `yaml:"topology" json:"topology"`
	Node        *NodePerformance     `yaml:"node,omitempty" json:"node,omitempty"`
	Edge        *EdgeNetwork         `yaml:"edge,omitempty" json:"edge,omitempty"`
	Resources   *ResourceConfig      `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type DomainTopologyConfig struct {
	Model           string   `yaml:"model" json:"model"`
	P               *float64 `yaml:"p,omitempty" json:"p,omitempty"`
	M               int      `yaml:"m,omitempty" json:"m,omitempty"`
	K               int      `yaml:"k,omitempty" json:"k,omitempty"`
	Beta            *float64 `yaml:"beta,omitempty" json:"beta,omitempty"`
	Rows            int      `yaml:"rows,omitempty" json:"rows,omitempty"`
	Columns         int      `yaml:"columns,omitempty" json:"columns,omitempty"`
	EnsureConnected bool     `yaml:"ensure_connected,omitempty" json:"ensure_connected,omitempty"`
}

type TopologyConfig struct {
	Directed bool       `yaml:"directed" json:"directed"`
	Nodes    []NodeSpec `yaml:"nodes" json:"nodes"`
	Matrix   [][]int    `yaml:"matrix,omitempty" json:"matrix,omitempty"`
	Edges    []EdgeSpec `yaml:"edges,omitempty" json:"edges,omitempty"`
}

type NodeSpec struct {
	ID          string           `yaml:"id" json:"id"`
	Performance *NodePerformance `yaml:"performance,omitempty" json:"performance,omitempty"`
	Resources   *ResourceConfig  `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type NodePerformance struct {
	ProcessingDelay Distribution `yaml:"processing_delay" json:"processing_delay"`
	Workers         int          `yaml:"workers" json:"workers"`
	QueueCapacity   int          `yaml:"queue_capacity" json:"queue_capacity"`
	OverflowPolicy  string       `yaml:"overflow_policy" json:"overflow_policy"`
}

type ResourceConfig struct {
	CPULimit      float64 `yaml:"cpu_limit,omitempty" json:"cpu_limit,omitempty"`
	MemoryLimitMB int     `yaml:"memory_limit_mb,omitempty" json:"memory_limit_mb,omitempty"`
}

type EdgeSpec struct {
	Source  string       `yaml:"source" json:"source"`
	Target  string       `yaml:"target" json:"target"`
	Network *EdgeNetwork `yaml:"network,omitempty" json:"network,omitempty"`
}

type EdgeNetwork struct {
	Delay         Distribution `yaml:"delay" json:"delay"`
	LossRate      *float64     `yaml:"loss_rate,omitempty" json:"loss_rate,omitempty"`
	BandwidthMbps *float64     `yaml:"bandwidth_mbps,omitempty" json:"bandwidth_mbps,omitempty"`
	QueueCapacity int          `yaml:"queue_capacity" json:"queue_capacity"`
}

type Distribution struct {
	Type     string  `yaml:"distribution" json:"distribution"`
	ValueMS  float64 `yaml:"value_ms,omitempty" json:"value_ms,omitempty"`
	MeanMS   float64 `yaml:"mean_ms,omitempty" json:"mean_ms,omitempty"`
	StdDevMS float64 `yaml:"stddev_ms,omitempty" json:"stddev_ms,omitempty"`
	MinMS    float64 `yaml:"min_ms,omitempty" json:"min_ms,omitempty"`
	MaxMS    float64 `yaml:"max_ms,omitempty" json:"max_ms,omitempty"`
	ScaleMS  float64 `yaml:"scale_ms,omitempty" json:"scale_ms,omitempty"`
	Shape    float64 `yaml:"shape,omitempty" json:"shape,omitempty"`
}

type TrafficConfig struct {
	Source           string `yaml:"source" json:"source"`
	StartAtMS        int64  `yaml:"start_at_ms" json:"start_at_ms"`
	Count            int    `yaml:"count" json:"count"`
	IntervalMS       int64  `yaml:"interval_ms" json:"interval_ms"`
	PayloadSizeBytes int    `yaml:"payload_size_bytes" json:"payload_size_bytes"`
}

type ResolvedEdgeNetwork struct {
	Delay         Distribution `yaml:"delay" json:"delay"`
	LossRate      float64      `yaml:"loss_rate" json:"loss_rate"`
	BandwidthMbps float64      `yaml:"bandwidth_mbps" json:"bandwidth_mbps"`
	QueueCapacity int          `yaml:"queue_capacity" json:"queue_capacity"`
}

type RuntimeNodeConfig struct {
	RunID          string                  `yaml:"run_id" json:"run_id"`
	ExperimentName string                  `yaml:"experiment_name" json:"experiment_name"`
	NodeID         string                  `yaml:"node_id" json:"node_id"`
	NodeIndex      int                     `yaml:"node_index" json:"node_index"`
	Seed           int64                   `yaml:"seed" json:"seed"`
	PrivateKey     string                  `yaml:"private_key" json:"private_key"`
	ListenAddress  string                  `yaml:"listen_address" json:"listen_address"`
	ControlAddress string                  `yaml:"control_address" json:"control_address"`
	ResultFile     string                  `yaml:"result_file" json:"result_file"`
	Performance    NodePerformance         `yaml:"performance" json:"performance"`
	Neighbors      []RuntimeNeighborConfig `yaml:"neighbors" json:"neighbors"`
}

type RuntimeNeighborConfig struct {
	NodeID  string              `yaml:"node_id" json:"node_id"`
	Index   int                 `yaml:"index" json:"index"`
	PeerID  string              `yaml:"peer_id" json:"peer_id"`
	Address string              `yaml:"address" json:"address"`
	Network ResolvedEdgeNetwork `yaml:"network" json:"network"`
}
