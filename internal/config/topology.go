package config

import "fmt"

func (s *Scenario) NormalizeTopology() error {
	matrix := s.Topology.Matrix
	if len(matrix) == 0 {
		return nil
	}
	if len(s.Topology.Edges) > 0 {
		return fmt.Errorf("topology.matrix and topology.edges cannot be used together")
	}
	n := len(s.Topology.Nodes)
	if len(matrix) != n {
		return fmt.Errorf("topology matrix has %d rows; expected %d", len(matrix), n)
	}
	for i := 0; i < n; i++ {
		if len(matrix[i]) != n {
			return fmt.Errorf("topology matrix row %d has %d columns; expected %d", i, len(matrix[i]), n)
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if matrix[i][j] != 0 && matrix[i][j] != 1 {
				return fmt.Errorf("topology matrix[%d][%d] must be 0 or 1", i, j)
			}
			if i == j && matrix[i][j] != 0 {
				return fmt.Errorf("topology matrix diagonal at [%d][%d] must be 0", i, j)
			}
			if matrix[i][j] != matrix[j][i] {
				return fmt.Errorf("topology matrix must be symmetric: [%d][%d] != [%d][%d]", i, j, j, i)
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if matrix[i][j] == 1 {
				s.Topology.Edges = append(s.Topology.Edges, EdgeSpec{
					Source: s.Topology.Nodes[i].ID,
					Target: s.Topology.Nodes[j].ID,
				})
			}
		}
	}
	s.Topology.Matrix = nil
	return nil
}
