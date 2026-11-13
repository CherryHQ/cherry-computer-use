package desktop

import "slices"

type treeTraits struct{ control, collection, hidden bool }
type plannedNode struct {
	ref, parent Ref
	path        []int
	depth       int
	collection  bool
}
type treePlan struct {
	nodes                          []plannedNode
	nodesTruncated, depthTruncated bool
}

// Select controls before large collections, then restore document order. The
// discovery budget bounds work as well as output, including hidden descendants.
func planTree(root Ref, rootIndex int, options TreeOptions, inspect func(Ref) (treeTraits, error), children func(Ref) ([]Ref, error)) (treePlan, error) {
	result := treePlan{}
	maxNodes, maxDepth := max(1, options.MaxNodes), max(0, options.MaxDepth)
	scanLimit := max(64, min(maxNodes, 2500)*4)
	var queues [5][]plannedNode
	var offsets [5]int
	seen := map[Ref]bool{}
	scanned := 0
	enqueue := func(work plannedNode) error {
		if work.depth > maxDepth {
			result.depthTruncated = true
			return nil
		}
		if scanned >= scanLimit {
			result.nodesTruncated = true
			return nil
		}
		if seen[work.ref] {
			return nil
		}
		seen[work.ref] = true
		scanned++
		traits, err := inspect(work.ref)
		if err != nil {
			return err
		}
		work.collection = work.collection || traits.collection
		rank := 1
		if traits.hidden {
			rank = 4
		} else if work.collection {
			rank = 3
			if traits.control {
				rank = 2
			}
		} else if traits.control {
			rank = 0
		}
		queues[rank] = append(queues[rank], work)
		return nil
	}
	if err := enqueue(plannedNode{ref: root, path: []int{rootIndex}}); err != nil {
		return result, err
	}
	for len(result.nodes) < maxNodes {
		rank := -1
		for i := range queues {
			if offsets[i] < len(queues[i]) {
				rank = i
				break
			}
		}
		if rank < 0 {
			break
		}
		work := queues[rank][offsets[rank]]
		offsets[rank]++
		result.nodes = append(result.nodes, work)
		refs, err := children(work.ref)
		if err != nil {
			return result, err
		}
		if work.depth >= maxDepth {
			result.depthTruncated = result.depthTruncated || len(refs) > 0
			continue
		}
		for i, ref := range refs {
			if scanned >= scanLimit {
				result.nodesTruncated = true
				break
			}
			path := append(slices.Clone(work.path), i)
			if err := enqueue(plannedNode{ref: ref, parent: work.ref, path: path, depth: work.depth + 1, collection: work.collection}); err != nil {
				return result, err
			}
		}
	}
	for i := range queues {
		result.nodesTruncated = result.nodesTruncated || offsets[i] < len(queues[i])
	}
	slices.SortFunc(result.nodes, func(a, b plannedNode) int { return slices.Compare(a.path, b.path) })
	return result, nil
}
