package metrics

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type MessageSummary struct {
	MessageID         string  `json:"message_id"`
	Origin            string  `json:"origin"`
	CreatedAtNS       int64   `json:"created_at_ns"`
	LastUniqueAtNS    int64   `json:"last_unique_at_ns"`
	ReachedNodes      int     `json:"reached_nodes"`
	TotalNodes        int     `json:"total_nodes"`
	Reachability      float64 `json:"reachability"`
	CompletionDelayMS float64 `json:"completion_delay_ms"`
	Transmissions     int     `json:"transmissions"`
	Duplicates        int     `json:"duplicates"`
	Drops             int     `json:"drops"`
	Suppressions      int     `json:"suppressions"`
	ControlSent       int     `json:"control_sent"`
	ControlReceived   int     `json:"control_received"`
	ControlDrops      int     `json:"control_drops"`
	ControlBytesSent  int     `json:"control_bytes_sent"`
}

type RunSummary struct {
	Protocol                 string  `json:"protocol"`
	Messages                 int     `json:"messages"`
	Nodes                    int     `json:"nodes"`
	AverageReachability      float64 `json:"average_reachability"`
	AverageCompletionDelayMS float64 `json:"average_completion_delay_ms"`
	TotalTransmissions       int     `json:"total_transmissions"`
	TotalDuplicates          int     `json:"total_duplicates"`
	TotalDrops               int     `json:"total_drops"`
	TotalSuppressions        int     `json:"total_suppressions"`
	TotalControlSent         int     `json:"total_control_sent"`
	TotalControlReceived     int     `json:"total_control_received"`
	TotalControlDrops        int     `json:"total_control_drops"`
	TotalControlBytesSent    int     `json:"total_control_bytes_sent"`
}

type messageAccumulator struct {
	origin        string
	createdAt     int64
	lastUniqueAt  int64
	reached       map[string]struct{}
	transmissions int
	duplicates    int
	drops         int
	suppressions  int
	controlSent   int
	controlRecv   int
	controlDrops  int
	controlBytes  int
}

func Aggregate(resultDir string, nodeCount int) (*RunSummary, error) {
	files, err := filepath.Glob(filepath.Join(resultDir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("list event files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no event files found in %s", resultDir)
	}

	messages := make(map[string]*messageAccumulator)
	protocol := ""
	for _, path := range files {
		if err := readEvents(path, func(event Event) {
			if protocol == "" && event.Protocol != "" {
				protocol = event.Protocol
			}
			if event.MessageID == "" {
				return
			}
			acc := messages[event.MessageID]
			if acc == nil {
				acc = &messageAccumulator{reached: make(map[string]struct{})}
				messages[event.MessageID] = acc
			}
			switch event.Type {
			case "message_created":
				acc.origin = event.Origin
				acc.createdAt = event.TimestampNS
				acc.lastUniqueAt = event.TimestampNS
				acc.reached[event.Node] = struct{}{}
			case "message_received":
				if event.Duplicate {
					acc.duplicates++
				} else {
					acc.reached[event.Node] = struct{}{}
					if event.TimestampNS > acc.lastUniqueAt {
						acc.lastUniqueAt = event.TimestampNS
					}
				}
			case "message_sent":
				acc.transmissions++
			case "message_dropped":
				acc.drops++
			case "message_suppressed":
				acc.suppressions++
			case "control_sent":
				acc.controlSent++
				acc.controlBytes += event.ControlBytes
			case "control_received":
				acc.controlRecv++
			case "control_dropped":
				acc.controlDrops++
			}
		}); err != nil {
			return nil, err
		}
	}

	rows := make([]MessageSummary, 0, len(messages))
	for id, acc := range messages {
		reachability := 0.0
		if nodeCount > 0 {
			reachability = float64(len(acc.reached)) / float64(nodeCount)
		}
		delayMS := 0.0
		if acc.createdAt > 0 && acc.lastUniqueAt >= acc.createdAt {
			delayMS = float64(acc.lastUniqueAt-acc.createdAt) / 1e6
		}
		rows = append(rows, MessageSummary{
			MessageID: id, Origin: acc.origin, CreatedAtNS: acc.createdAt,
			LastUniqueAtNS: acc.lastUniqueAt, ReachedNodes: len(acc.reached),
			TotalNodes: nodeCount, Reachability: reachability,
			CompletionDelayMS: delayMS, Transmissions: acc.transmissions,
			Duplicates: acc.duplicates, Drops: acc.drops,
			Suppressions: acc.suppressions, ControlSent: acc.controlSent,
			ControlReceived: acc.controlRecv, ControlDrops: acc.controlDrops,
			ControlBytesSent: acc.controlBytes,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAtNS == rows[j].CreatedAtNS {
			return rows[i].MessageID < rows[j].MessageID
		}
		return rows[i].CreatedAtNS < rows[j].CreatedAtNS
	})

	summary := &RunSummary{Protocol: protocol, Messages: len(rows), Nodes: nodeCount}
	for _, row := range rows {
		summary.AverageReachability += row.Reachability
		summary.AverageCompletionDelayMS += row.CompletionDelayMS
		summary.TotalTransmissions += row.Transmissions
		summary.TotalDuplicates += row.Duplicates
		summary.TotalDrops += row.Drops
		summary.TotalSuppressions += row.Suppressions
		summary.TotalControlSent += row.ControlSent
		summary.TotalControlReceived += row.ControlReceived
		summary.TotalControlDrops += row.ControlDrops
		summary.TotalControlBytesSent += row.ControlBytesSent
	}
	if len(rows) > 0 {
		summary.AverageReachability /= float64(len(rows))
		summary.AverageCompletionDelayMS /= float64(len(rows))
	}

	if err := writeMessageCSV(filepath.Join(resultDir, "messages.csv"), rows); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(resultDir, "summary.json"), data, 0o644); err != nil {
		return nil, err
	}
	return summary, nil
}

func readEvents(path string, consume func(Event)) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event in %s: %w", path, err)
		}
		consume(event)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func writeMessageCSV(path string, rows []MessageSummary) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"message_id", "origin", "created_at_ns", "last_unique_at_ns", "reached_nodes", "total_nodes", "reachability", "completion_delay_ms", "transmissions", "duplicates", "drops", "suppressions", "control_sent", "control_received", "control_drops", "control_bytes_sent"}); err != nil {
		return err
	}
	for _, row := range rows {
		record := []string{
			row.MessageID, row.Origin,
			strconv.FormatInt(row.CreatedAtNS, 10), strconv.FormatInt(row.LastUniqueAtNS, 10),
			strconv.Itoa(row.ReachedNodes), strconv.Itoa(row.TotalNodes),
			strconv.FormatFloat(row.Reachability, 'f', 6, 64),
			strconv.FormatFloat(row.CompletionDelayMS, 'f', 3, 64),
			strconv.Itoa(row.Transmissions), strconv.Itoa(row.Duplicates), strconv.Itoa(row.Drops),
			strconv.Itoa(row.Suppressions), strconv.Itoa(row.ControlSent),
			strconv.Itoa(row.ControlReceived), strconv.Itoa(row.ControlDrops),
			strconv.Itoa(row.ControlBytesSent),
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	return writer.Error()
}
