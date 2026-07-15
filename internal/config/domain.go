package config

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

type generatedEdge struct {
	a int
	b int
}

// ExpandDomain expands a compact domain declaration into the explicit node and
// edge representation used by the rest of peerkit. Generation is deterministic
// for a given experiment seed and domain definition.
func (s *Scenario) ExpandDomain() error {
	if s.Domain == nil {
		return nil
	}
	if len(s.Topology.Nodes) > 0 || len(s.Topology.Edges) > 0 || len(s.Topology.Matrix) > 0 {
		return fmt.Errorf("domain and explicit topology cannot be used together")
	}

	domain := s.Domain
	if domain.N > 0 && domain.NodeCount > 0 && domain.N != domain.NodeCount {
		return fmt.Errorf("domain.n and domain.node_count disagree")
	}
	nodeCount := domain.N
	if nodeCount == 0 {
		nodeCount = domain.NodeCount
	}
	if nodeCount <= 0 {
		return fmt.Errorf("domain.n must be positive")
	}
	idPrefix := domain.IDPrefix
	if idPrefix == "" {
		idPrefix = "n"
	}
	zeroPadding := domain.ZeroPadding
	if zeroPadding < 0 {
		return fmt.Errorf("domain.zero_padding must be non-negative")
	}
	if zeroPadding == 0 {
		zeroPadding = len(strconv.Itoa(nodeCount - 1))
		if zeroPadding < 1 {
			zeroPadding = 1
		}
	}

	// Domain-level performance settings override top-level defaults while still
	// inheriting fields omitted by the compact declaration.
	if domain.Node != nil {
		resolved := *domain.Node
		mergeNodePerformance(&resolved, s.Defaults.Node)
		s.Defaults.Node = resolved
	}
	if domain.Edge != nil {
		resolved := cloneEdgeNetwork(*domain.Edge)
		mergeEdgeNetwork(&resolved, s.Defaults.Edge)
		s.Defaults.Edge = resolved
	}

	nodes := make([]NodeSpec, nodeCount)
	for i := range nodes {
		id := fmt.Sprintf("%s%0*d", idPrefix, zeroPadding, i)
		nodes[i] = NodeSpec{ID: id}
		if domain.Resources != nil {
			resources := *domain.Resources
			nodes[i].Resources = &resources
		}
	}

	rng := rand.New(rand.NewSource(s.Experiment.Seed))
	generated, err := generateDomainEdges(nodeCount, domain.Topology, rng)
	if err != nil {
		return err
	}
	if domain.Topology.EnsureConnected {
		generated = connectGeneratedComponents(nodeCount, generated, rng)
	}
	generated = normalizeGeneratedEdges(generated)

	edges := make([]EdgeSpec, 0, len(generated))
	for _, edge := range generated {
		edges = append(edges, EdgeSpec{
			Source: nodes[edge.a].ID,
			Target: nodes[edge.b].ID,
		})
	}

	s.Topology = TopologyConfig{
		Directed: false,
		Nodes:    nodes,
		Edges:    edges,
	}
	return nil
}

func cloneEdgeNetwork(value EdgeNetwork) EdgeNetwork {
	cloned := value
	if value.LossRate != nil {
		v := *value.LossRate
		cloned.LossRate = &v
	}
	if value.BandwidthMbps != nil {
		v := *value.BandwidthMbps
		cloned.BandwidthMbps = &v
	}
	return cloned
}

func generateDomainEdges(n int, cfg DomainTopologyConfig, rng *rand.Rand) ([]generatedEdge, error) {
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	switch model {
	case "er", "erdos-renyi", "erdos_renyi", "gnp":
		if cfg.P == nil {
			return nil, fmt.Errorf("domain.topology.p is required for ER")
		}
		if *cfg.P < 0 || *cfg.P > 1 {
			return nil, fmt.Errorf("domain.topology.p must be between 0 and 1")
		}
		return generateER(n, *cfg.P, rng), nil

	case "ba", "barabasi-albert", "barabasi_albert":
		if cfg.M <= 0 {
			return nil, fmt.Errorf("domain.topology.m must be positive for BA")
		}
		if cfg.M >= n {
			return nil, fmt.Errorf("domain.topology.m must be smaller than node count for BA")
		}
		return generateBA(n, cfg.M, rng), nil

	case "ws", "watts-strogatz", "watts_strogatz":
		if cfg.K <= 0 || cfg.K >= n || cfg.K%2 != 0 {
			return nil, fmt.Errorf("domain.topology.k must be positive, even, and smaller than node count for WS")
		}
		if cfg.Beta == nil {
			return nil, fmt.Errorf("domain.topology.beta is required for WS")
		}
		if *cfg.Beta < 0 || *cfg.Beta > 1 {
			return nil, fmt.Errorf("domain.topology.beta must be between 0 and 1")
		}
		return generateWS(n, cfg.K, *cfg.Beta, rng), nil

	case "ring", "cycle":
		if n < 3 {
			return nil, fmt.Errorf("ring topology requires at least 3 nodes")
		}
		return generateRing(n), nil

	case "path", "line":
		return generatePath(n), nil

	case "complete", "clique":
		return generateComplete(n), nil

	case "grid", "mesh":
		return generateGrid(n, cfg.Rows, cfg.Columns)

	default:
		return nil, fmt.Errorf("unsupported domain topology model %q", cfg.Model)
	}
}

func generateER(n int, p float64, rng *rand.Rand) []generatedEdge {
	edges := make([]generatedEdge, 0)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if rng.Float64() < p {
				edges = append(edges, generatedEdge{a: i, b: j})
			}
		}
	}
	return edges
}

// generateBA starts with a clique of m+1 nodes and attaches every subsequent
// node to m distinct existing nodes with probability proportional to degree.
func generateBA(n, m int, rng *rand.Rand) []generatedEdge {
	initial := m + 1
	edges := make([]generatedEdge, 0, m*n)
	degrees := make([]int, n)
	adjacency := make([]map[int]struct{}, n)
	for i := range adjacency {
		adjacency[i] = make(map[int]struct{})
	}

	for i := 0; i < initial; i++ {
		for j := i + 1; j < initial; j++ {
			addGeneratedEdge(&edges, adjacency, degrees, i, j)
		}
	}

	for node := initial; node < n; node++ {
		selected := make(map[int]struct{}, m)
		for len(selected) < m {
			target := weightedDegreeChoice(degrees[:node], selected, rng)
			selected[target] = struct{}{}
		}
		for target := range selected {
			addGeneratedEdge(&edges, adjacency, degrees, node, target)
		}
	}
	return edges
}

func weightedDegreeChoice(degrees []int, excluded map[int]struct{}, rng *rand.Rand) int {
	total := 0
	for node, degree := range degrees {
		if _, skip := excluded[node]; !skip {
			total += degree
		}
	}
	if total <= 0 {
		for {
			candidate := rng.Intn(len(degrees))
			if _, skip := excluded[candidate]; !skip {
				return candidate
			}
		}
	}
	choice := rng.Intn(total)
	for node, degree := range degrees {
		if _, skip := excluded[node]; skip {
			continue
		}
		if choice < degree {
			return node
		}
		choice -= degree
	}
	panic("unreachable weighted degree selection")
}

func generateWS(n, k int, beta float64, rng *rand.Rand) []generatedEdge {
	adjacency := make([]map[int]struct{}, n)
	for i := range adjacency {
		adjacency[i] = make(map[int]struct{})
	}
	edges := make([]generatedEdge, 0, n*k/2)

	for i := 0; i < n; i++ {
		for distance := 1; distance <= k/2; distance++ {
			j := (i + distance) % n
			addSimpleGeneratedEdge(&edges, adjacency, i, j)
		}
	}

	for i := 0; i < n; i++ {
		for distance := 1; distance <= k/2; distance++ {
			original := (i + distance) % n
			if rng.Float64() >= beta {
				continue
			}
			if _, exists := adjacency[i][original]; !exists {
				continue
			}

			candidates := make([]int, 0, n-len(adjacency[i])-1)
			for candidate := 0; candidate < n; candidate++ {
				if candidate == i {
					continue
				}
				if _, exists := adjacency[i][candidate]; !exists {
					candidates = append(candidates, candidate)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			newTarget := candidates[rng.Intn(len(candidates))]
			removeSimpleGeneratedEdge(&edges, adjacency, i, original)
			addSimpleGeneratedEdge(&edges, adjacency, i, newTarget)
		}
	}
	return edges
}

func generateRing(n int) []generatedEdge {
	edges := make([]generatedEdge, 0, n)
	for i := 0; i < n; i++ {
		edges = append(edges, canonicalGeneratedEdge(i, (i+1)%n))
	}
	return edges
}

func generatePath(n int) []generatedEdge {
	edges := make([]generatedEdge, 0, maxInt(0, n-1))
	for i := 0; i+1 < n; i++ {
		edges = append(edges, generatedEdge{a: i, b: i + 1})
	}
	return edges
}

func generateComplete(n int) []generatedEdge {
	edges := make([]generatedEdge, 0, n*(n-1)/2)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			edges = append(edges, generatedEdge{a: i, b: j})
		}
	}
	return edges
}

func generateGrid(n, rows, columns int) ([]generatedEdge, error) {
	if rows <= 0 && columns <= 0 {
		return nil, fmt.Errorf("grid topology requires rows and/or columns")
	}
	if rows <= 0 {
		if n%columns != 0 {
			return nil, fmt.Errorf("node count %d is not divisible by grid columns %d", n, columns)
		}
		rows = n / columns
	}
	if columns <= 0 {
		if n%rows != 0 {
			return nil, fmt.Errorf("node count %d is not divisible by grid rows %d", n, rows)
		}
		columns = n / rows
	}
	if rows*columns != n {
		return nil, fmt.Errorf("grid rows*columns=%d does not match node count %d", rows*columns, n)
	}

	edges := make([]generatedEdge, 0, rows*(columns-1)+(rows-1)*columns)
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			current := row*columns + column
			if column+1 < columns {
				edges = append(edges, generatedEdge{a: current, b: current + 1})
			}
			if row+1 < rows {
				edges = append(edges, generatedEdge{a: current, b: current + columns})
			}
		}
	}
	return edges, nil
}

func connectGeneratedComponents(n int, edges []generatedEdge, rng *rand.Rand) []generatedEdge {
	components := generatedComponents(n, edges)
	if len(components) <= 1 {
		return edges
	}
	// Randomize one representative per component and connect those representatives
	// as a chain. Exactly component_count-1 edges are added.
	representatives := make([]int, len(components))
	for i, component := range components {
		representatives[i] = component[rng.Intn(len(component))]
	}
	rng.Shuffle(len(representatives), func(i, j int) {
		representatives[i], representatives[j] = representatives[j], representatives[i]
	})
	for i := 0; i+1 < len(representatives); i++ {
		edges = append(edges, canonicalGeneratedEdge(representatives[i], representatives[i+1]))
	}
	return edges
}

func generatedComponents(n int, edges []generatedEdge) [][]int {
	adjacency := make([][]int, n)
	for _, edge := range edges {
		adjacency[edge.a] = append(adjacency[edge.a], edge.b)
		adjacency[edge.b] = append(adjacency[edge.b], edge.a)
	}
	visited := make([]bool, n)
	components := make([][]int, 0)
	for start := 0; start < n; start++ {
		if visited[start] {
			continue
		}
		component := make([]int, 0)
		stack := []int{start}
		visited[start] = true
		for len(stack) > 0 {
			last := len(stack) - 1
			node := stack[last]
			stack = stack[:last]
			component = append(component, node)
			for _, neighbor := range adjacency[node] {
				if !visited[neighbor] {
					visited[neighbor] = true
					stack = append(stack, neighbor)
				}
			}
		}
		components = append(components, component)
	}
	return components
}

func normalizeGeneratedEdges(edges []generatedEdge) []generatedEdge {
	unique := make(map[generatedEdge]struct{}, len(edges))
	for _, edge := range edges {
		if edge.a == edge.b {
			continue
		}
		unique[canonicalGeneratedEdge(edge.a, edge.b)] = struct{}{}
	}
	result := make([]generatedEdge, 0, len(unique))
	for edge := range unique {
		result = append(result, edge)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].a != result[j].a {
			return result[i].a < result[j].a
		}
		return result[i].b < result[j].b
	})
	return result
}

func canonicalGeneratedEdge(a, b int) generatedEdge {
	if a < b {
		return generatedEdge{a: a, b: b}
	}
	return generatedEdge{a: b, b: a}
}

func addGeneratedEdge(edges *[]generatedEdge, adjacency []map[int]struct{}, degrees []int, a, b int) {
	if _, exists := adjacency[a][b]; exists {
		return
	}
	*edges = append(*edges, canonicalGeneratedEdge(a, b))
	adjacency[a][b] = struct{}{}
	adjacency[b][a] = struct{}{}
	degrees[a]++
	degrees[b]++
}

func addSimpleGeneratedEdge(edges *[]generatedEdge, adjacency []map[int]struct{}, a, b int) {
	if _, exists := adjacency[a][b]; exists || a == b {
		return
	}
	*edges = append(*edges, canonicalGeneratedEdge(a, b))
	adjacency[a][b] = struct{}{}
	adjacency[b][a] = struct{}{}
}

func removeSimpleGeneratedEdge(edges *[]generatedEdge, adjacency []map[int]struct{}, a, b int) {
	canonical := canonicalGeneratedEdge(a, b)
	for i, edge := range *edges {
		if edge == canonical {
			(*edges)[i] = (*edges)[len(*edges)-1]
			*edges = (*edges)[:len(*edges)-1]
			break
		}
	}
	delete(adjacency[a], b)
	delete(adjacency[b], a)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
