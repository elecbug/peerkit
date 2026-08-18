package config

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

type generatedEdge struct {
	a int
	b int

	// delayBaselineMS is populated by topology generators that make the
	// network delay part of the attachment decision. Ordinary generators leave
	// it unset and ResolveDelayAssignments samples the edge baseline later.
	delayBaselineMS  float64
	hasDelayBaseline bool
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
	// inheriting fields omitted by the compact declaration. Ordinary topologies
	// assign stable baselines after expansion; performance-aware topologies such
	// as BA-opportunistic and DRS resolve node baselines during expansion because
	// they participate in topology construction.
	var domainNodeTemplate *NodePerformance
	if domain.Node != nil {
		resolved := *domain.Node
		mergeNodePerformance(&resolved, s.Defaults.Node)
		s.Defaults.Node = resolved
		domainNodeTemplate = &resolved
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
		if domainNodeTemplate != nil {
			performance := *domainNodeTemplate
			nodes[i].Performance = &performance
		}
		if domain.Resources != nil {
			resources := *domain.Resources
			nodes[i].Resources = &resources
		}
	}

	topologyRNG := rand.New(rand.NewSource(s.Experiment.Seed))
	generated, err := generateDomainEdges(
		nodeCount,
		domain.Topology,
		topologyRNG,
		nodes,
		s.Defaults.Edge,
		s.Experiment.Seed,
	)
	if err != nil {
		return err
	}
	if domain.Topology.EnsureConnected {
		generated = connectGeneratedComponents(nodeCount, generated, topologyRNG)
	}
	generated = normalizeGeneratedEdges(generated)

	edges := make([]EdgeSpec, 0, len(generated))
	for _, edge := range generated {
		spec := EdgeSpec{
			Source: nodes[edge.a].ID,
			Target: nodes[edge.b].ID,
		}
		if edge.hasDelayBaseline {
			network := cloneEdgeNetwork(s.Defaults.Edge)
			network.DelayDistribution = constantDistribution(edge.delayBaselineMS)
			spec.Network = &network
		}
		edges = append(edges, spec)
	}

	s.Topology = TopologyConfig{
		Directed: false,
		Nodes:    nodes,
		Edges:    edges,
	}
	return nil
}

func domainSeed(base int64, purpose string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("peerkit-domain-" + purpose))
	return base ^ int64(h.Sum64())
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

func generateDomainEdges(
	n int,
	cfg DomainTopologyConfig,
	rng *rand.Rand,
	nodes []NodeSpec,
	edgeTemplate EdgeNetwork,
	seed int64,
) ([]generatedEdge, error) {
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	switch model {
	case "er", "erdos-renyi", "erdos_renyi", "gnp":
		if cfg.P != nil && cfg.AverageDegree != nil {
			return nil, fmt.Errorf("domain.topology.p and average_degree cannot be used together")
		}
		p := 0.0
		switch {
		case cfg.AverageDegree != nil:
			if *cfg.AverageDegree < 0 || *cfg.AverageDegree > float64(n-1) {
				return nil, fmt.Errorf("domain.topology.average_degree must be between 0 and n-1")
			}
			if n > 1 {
				p = *cfg.AverageDegree / float64(n-1)
			}
		case cfg.P != nil:
			p = *cfg.P
		default:
			return nil, fmt.Errorf("domain.topology.p or average_degree is required for ER")
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("domain.topology.p must be between 0 and 1")
		}
		return generateER(n, p, rng), nil

	case "ba", "barabasi-albert", "barabasi_albert":
		if cfg.M <= 0 {
			return nil, fmt.Errorf("domain.topology.m must be positive for BA")
		}
		if cfg.M >= n {
			return nil, fmt.Errorf("domain.topology.m must be smaller than node count for BA")
		}
		return generateBA(n, cfg.M, rng), nil

	case "ba-opportunistic", "ba_opportunistic", "barabasi-albert-opportunistic", "barabasi_albert_opportunistic":
		if cfg.M <= 0 {
			return nil, fmt.Errorf("domain.topology.m must be positive for BA-opportunistic")
		}
		if cfg.M >= n {
			return nil, fmt.Errorf("domain.topology.m must be smaller than node count for BA-opportunistic")
		}
		if len(nodes) != n {
			return nil, fmt.Errorf("BA-opportunistic requires %d resolved nodes; got %d", n, len(nodes))
		}
		if err := resolveNodeDelayAssignments(nodes, seed); err != nil {
			return nil, fmt.Errorf("resolve BA-opportunistic node delays: %w", err)
		}
		return generateBAOpportunistic(n, cfg.M, nodes, edgeTemplate, seed)

	case "drs", "double-ring-smallworld", "double_ring_smallworld":
		if n < 7 {
			return nil, fmt.Errorf("DRS topology requires at least 7 nodes")
		}
		alpha := 0.25
		if cfg.Alpha != nil {
			alpha = *cfg.Alpha
		}
		if alpha <= 0 || alpha >= 1 {
			return nil, fmt.Errorf("domain.topology.alpha must be between 0 and 1 (exclusive) for DRS")
		}
		if cfg.Beta == nil {
			return nil, fmt.Errorf("domain.topology.beta is required for DRS")
		}
		if *cfg.Beta <= 0 || math.Trunc(*cfg.Beta) != *cfg.Beta {
			return nil, fmt.Errorf("domain.topology.beta must be a positive integer for DRS")
		}
		if len(nodes) != n {
			return nil, fmt.Errorf("DRS requires %d resolved nodes; got %d", n, len(nodes))
		}
		if err := resolveNodeDelayAssignments(nodes, seed); err != nil {
			return nil, fmt.Errorf("resolve DRS node delays: %w", err)
		}
		return generateDRS(n, alpha, int(*cfg.Beta), nodes, rng)

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

// generateBAOpportunistic keeps the BA growth process, but makes both the
// initial hub candidates and later preferential-attachment decisions
// performance-aware.
//
// Stable node processing baselines are resolved before topology generation.
// The initial K_(m+1) clique is selected greedily: after the first (lowest
// processing-delay) node, each next seed minimizes
//
//	processingBaseline(candidate) + mean(linkBaseline(candidate, clique))
//
// Later nodes attach to m existing nodes with weight
//
//	degree(target) / (1ms + processingBaseline(target) + edgeBaseline)
//
// Candidate edge baselines are sampled once from delay_distribution and cached;
// if an edge is selected, that same value becomes its stable runtime baseline.
// Thus fast-processing nodes with favorable links are preferentially placed in
// hub positions, while the degree term retains the BA rich-get-richer effect.
func generateBAOpportunistic(
	n, m int,
	nodes []NodeSpec,
	edgeTemplate EdgeNetwork,
	seed int64,
) ([]generatedEdge, error) {
	if err := validateDistribution(edgeTemplate.DelayDistribution); err != nil {
		return nil, fmt.Errorf("BA-opportunistic edge delay_distribution: %w", err)
	}

	processingBaselines := make([]float64, n)
	for index, node := range nodes {
		if node.Performance == nil {
			return nil, fmt.Errorf("BA-opportunistic node %s has no resolved performance config", node.ID)
		}
		delay := node.Performance.ProcessingDelayDistribution
		if strings.ToLower(delay.Type) != "constant" {
			return nil, fmt.Errorf("BA-opportunistic node %s processing baseline is not resolved", node.ID)
		}
		processingBaselines[index] = delay.ValueMS
	}

	edgeRNG := rand.New(rand.NewSource(domainSeed(seed, "ba-opportunistic-edge-candidates")))
	choiceRNG := rand.New(rand.NewSource(domainSeed(seed, "ba-opportunistic-attachment")))

	type pairKey struct {
		a int
		b int
	}
	pairDelays := make(map[pairKey]float64)
	pairDelay := func(a, b int) float64 {
		if a > b {
			a, b = b, a
		}
		key := pairKey{a: a, b: b}
		if delay, ok := pairDelays[key]; ok {
			return delay
		}
		delay := edgeTemplate.DelayDistribution.SampleMilliseconds(edgeRNG)
		pairDelays[key] = delay
		return delay
	}

	initial := m + 1
	initialNodes := selectOpportunisticInitialClique(n, initial, processingBaselines, pairDelay)
	inInitial := make(map[int]struct{}, initial)
	for _, node := range initialNodes {
		inInitial[node] = struct{}{}
	}

	edges := make([]generatedEdge, 0, m*n)
	degrees := make([]int, n)
	adjacency := make([]map[int]struct{}, n)
	for i := range adjacency {
		adjacency[i] = make(map[int]struct{})
	}

	for i := 0; i < len(initialNodes); i++ {
		for j := i + 1; j < len(initialNodes); j++ {
			a := initialNodes[i]
			b := initialNodes[j]
			addGeneratedEdgeWithDelay(&edges, adjacency, degrees, a, b, pairDelay(a, b))
		}
	}

	// The initial clique is the earliest BA population. All other logical nodes
	// then arrive in stable ID/index order.
	existing := append([]int(nil), initialNodes...)
	for node := 0; node < n; node++ {
		if _, skip := inInitial[node]; skip {
			continue
		}

		candidateDelays := make([]float64, n)
		for _, target := range existing {
			candidateDelays[target] = pairDelay(node, target)
		}

		selected := make(map[int]struct{}, m)
		for len(selected) < m {
			target := weightedOpportunityChoice(
				existing,
				degrees,
				processingBaselines,
				candidateDelays,
				selected,
				choiceRNG,
			)
			selected[target] = struct{}{}
		}
		for target := range selected {
			addGeneratedEdgeWithDelay(
				&edges,
				adjacency,
				degrees,
				node,
				target,
				candidateDelays[target],
			)
		}
		existing = append(existing, node)
	}
	return edges, nil
}

func selectOpportunisticInitialClique(
	n int,
	initial int,
	processingBaselines []float64,
	pairDelay func(int, int) float64,
) []int {
	selected := make([]int, 0, initial)
	chosen := make(map[int]struct{}, initial)

	// There is no link to evaluate for the very first node, so begin with the
	// lowest processing baseline. Ties are resolved by the stable node index.
	first := 0
	for candidate := 1; candidate < n; candidate++ {
		if processingBaselines[candidate] < processingBaselines[first] {
			first = candidate
		}
	}
	selected = append(selected, first)
	chosen[first] = struct{}{}

	for len(selected) < initial {
		bestNode := -1
		bestScore := 0.0
		for candidate := 0; candidate < n; candidate++ {
			if _, exists := chosen[candidate]; exists {
				continue
			}
			linkTotal := 0.0
			for _, existing := range selected {
				linkTotal += pairDelay(candidate, existing)
			}
			linkMean := linkTotal / float64(len(selected))
			score := processingBaselines[candidate] + linkMean
			if bestNode < 0 || score < bestScore || (score == bestScore && candidate < bestNode) {
				bestNode = candidate
				bestScore = score
			}
		}
		selected = append(selected, bestNode)
		chosen[bestNode] = struct{}{}
	}
	return selected
}

func weightedOpportunityChoice(
	candidates []int,
	degrees []int,
	processingBaselines []float64,
	edgeBaselines []float64,
	excluded map[int]struct{},
	rng *rand.Rand,
) int {
	weights := make([]float64, len(candidates))
	total := 0.0
	for index, node := range candidates {
		if _, skip := excluded[node]; skip {
			continue
		}
		// The 1ms floor prevents a zero-delay candidate from producing an
		// infinite weight while remaining negligible at ordinary WAN delays.
		costMS := 1.0 + processingBaselines[node] + edgeBaselines[node]
		weight := float64(degrees[node]) / costMS
		weights[index] = weight
		total += weight
	}
	if total <= 0 {
		for {
			candidate := candidates[rng.Intn(len(candidates))]
			if _, skip := excluded[candidate]; !skip {
				return candidate
			}
		}
	}

	choice := rng.Float64() * total
	for index, node := range candidates {
		if _, skip := excluded[node]; skip {
			continue
		}
		weight := weights[index]
		if choice < weight {
			return node
		}
		choice -= weight
	}

	// Floating-point rounding can leave choice infinitesimally above zero.
	for index := len(candidates) - 1; index >= 0; index-- {
		node := candidates[index]
		if _, skip := excluded[node]; !skip {
			return node
		}
	}
	panic("unreachable opportunistic selection")
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

// generateDRS builds a Double-Ring Small-World (DRS) topology.
//
// Nodes are ranked by their already-resolved processing-delay baseline. The
// fastest ceil(alpha * n) nodes form the inner/core ring and the remaining
// nodes form the outer/periphery ring. Alpha defaults to 0.25 when omitted.
// Ties are broken by the stable node index.
// Within each ring, node indices are used as the deterministic ring order.
//
// Every node then selects beta distinct nodes from the inner ring and forms an
// undirected link to each selected target. For inner-ring nodes, self and the
// two existing ring neighbors are excluded, so beta denotes additional core
// shortcuts rather than re-selecting a ring edge. Because the graph is
// undirected, reciprocal selections collapse to one physical edge, but each
// node still has all of its beta selected core targets as neighbors.
//
// DRS intentionally uses node processing performance only for core membership;
// edge delay baselines are assigned normally after topology expansion.
func generateDRS(n int, alpha float64, beta int, nodes []NodeSpec, rng *rand.Rand) ([]generatedEdge, error) {
	if alpha <= 0 || alpha >= 1 {
		return nil, fmt.Errorf("DRS alpha must be between 0 and 1 (exclusive)")
	}
	coreCount := int(math.Ceil(alpha * float64(n)))
	if coreCount < 4 || n-coreCount < 3 {
		return nil, fmt.Errorf(
			"DRS alpha=%.4g yields %d inner-ring and %d outer-ring nodes; need at least 4 inner and 3 outer nodes",
			alpha,
			coreCount,
			n-coreCount,
		)
	}
	if beta <= 0 {
		return nil, fmt.Errorf("DRS beta must be positive")
	}
	if beta > coreCount-3 {
		return nil, fmt.Errorf(
			"DRS beta=%d is too large for %d inner-ring nodes; maximum additional core fanout is %d",
			beta,
			coreCount,
			coreCount-3,
		)
	}
	if len(nodes) != n {
		return nil, fmt.Errorf("DRS requires %d resolved nodes; got %d", n, len(nodes))
	}

	ranked := make([]int, n)
	processingBaselines := make([]float64, n)
	for index, node := range nodes {
		if node.Performance == nil {
			return nil, fmt.Errorf("DRS node %s has no resolved performance config", node.ID)
		}
		delay := node.Performance.ProcessingDelayDistribution
		if strings.ToLower(delay.Type) != "constant" {
			return nil, fmt.Errorf("DRS node %s processing baseline is not resolved", node.ID)
		}
		ranked[index] = index
		processingBaselines[index] = delay.ValueMS
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left := ranked[i]
		right := ranked[j]
		if processingBaselines[left] != processingBaselines[right] {
			return processingBaselines[left] < processingBaselines[right]
		}
		return left < right
	})

	inner := append([]int(nil), ranked[:coreCount]...)
	outer := append([]int(nil), ranked[coreCount:]...)
	// Ring order is intentionally independent of the performance ranking once
	// membership is decided, keeping topology generation deterministic and
	// avoiding an extra performance-ordering effect inside either ring.
	sort.Ints(inner)
	sort.Ints(outer)

	adjacency := make([]map[int]struct{}, n)
	ringAdjacency := make([]map[int]struct{}, n)
	for i := 0; i < n; i++ {
		adjacency[i] = make(map[int]struct{})
		ringAdjacency[i] = make(map[int]struct{})
	}
	edges := make([]generatedEdge, 0, n*(beta+2))

	addRing := func(ring []int) {
		for i, node := range ring {
			next := ring[(i+1)%len(ring)]
			addSimpleGeneratedEdge(&edges, adjacency, node, next)
			ringAdjacency[node][next] = struct{}{}
			ringAdjacency[next][node] = struct{}{}
		}
	}
	addRing(inner)
	addRing(outer)

	for node := 0; node < n; node++ {
		candidates := make([]int, 0, len(inner))
		for _, target := range inner {
			if target == node {
				continue
			}
			if _, isRingNeighbor := ringAdjacency[node][target]; isRingNeighbor {
				continue
			}
			candidates = append(candidates, target)
		}
		if len(candidates) < beta {
			return nil, fmt.Errorf(
				"DRS node %s has only %d eligible inner-ring targets for beta=%d",
				nodes[node].ID,
				len(candidates),
				beta,
			)
		}
		rng.Shuffle(len(candidates), func(i, j int) {
			candidates[i], candidates[j] = candidates[j], candidates[i]
		})
		for _, target := range candidates[:beta] {
			addSimpleGeneratedEdge(&edges, adjacency, node, target)
		}
	}

	return edges, nil
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
	type edgeKey struct {
		a int
		b int
	}
	unique := make(map[edgeKey]generatedEdge, len(edges))
	for _, edge := range edges {
		if edge.a == edge.b {
			continue
		}
		edge = canonicalizeGeneratedEdge(edge)
		key := edgeKey{a: edge.a, b: edge.b}
		if _, exists := unique[key]; !exists {
			unique[key] = edge
		}
	}
	result := make([]generatedEdge, 0, len(unique))
	for _, edge := range unique {
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
	return canonicalizeGeneratedEdge(generatedEdge{a: a, b: b})
}

func canonicalizeGeneratedEdge(edge generatedEdge) generatedEdge {
	if edge.a <= edge.b {
		return edge
	}
	edge.a, edge.b = edge.b, edge.a
	return edge
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

func addGeneratedEdgeWithDelay(
	edges *[]generatedEdge,
	adjacency []map[int]struct{},
	degrees []int,
	a, b int,
	delayBaselineMS float64,
) {
	if _, exists := adjacency[a][b]; exists {
		return
	}
	edge := canonicalizeGeneratedEdge(generatedEdge{
		a:                a,
		b:                b,
		delayBaselineMS:  delayBaselineMS,
		hasDelayBaseline: true,
	})
	*edges = append(*edges, edge)
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
