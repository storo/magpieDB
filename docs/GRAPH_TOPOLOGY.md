# HNSW Graph Topology and Invariants

## Overview

This document describes the graph topology properties maintained by MagpieDB's HNSW index implementation, especially during and after vector removal operations.

## HNSW Graph Structure

### Hierarchical Layers

```
Level 3:  [Entry] -------- [A]
             |              |
Level 2:  [Entry] -- [B] -- [A] -- [C]
             |       |       |      |
Level 1:  [Entry]-[B]-[D]-[A]-[E]-[C]-[F]
             |     | | | | | | | |  |  |
Level 0:  [Entry][B][D][A][E][C][F][G][H][I][J]...
```

### Key Properties

1. **Layer 0**: Contains ALL nodes
2. **Higher Layers**: Exponentially fewer nodes (probability ~0.5)
3. **Entry Point**: Highest level node in the graph
4. **Connections**: Each node has ≤M neighbors at higher layers, ≤2M at layer 0

## Invariants Maintained

### 1. Node-Level Invariants

**For each node `n`**:
- `n.Level` ≥ 0
- `len(n.Neighbors)` = `n.Level + 1`
- All nodes exist in layer 0
- Nodes only exist in layers 0 through `n.Level`

**Code Verification**:
```go
func verifyNodeInvariants(node *HSNWNode) bool {
    if node.Level < 0 {
        return false
    }
    if len(node.Neighbors) != node.Level+1 {
        return false
    }
    for level := 0; level <= node.Level; level++ {
        if len(node.Neighbors[level]) > maxConnectionsForLevel(level) {
            return false
        }
    }
    return true
}
```

### 2. Connectivity Invariants

**Bidirectional Links**:
- If node A has B as neighbor at level L, then B should have A (eventual consistency)
- After `Remove()`, all references to deleted node are cleaned
- After `reconnectNeighbors()`, deleted node's neighbors are connected to each other

**No Dangling References**:
- Every neighbor ID must point to an existing node
- `Diagnostics()` can detect violations
- `OptimizeIndex()` cleans up dangling references

### 3. Entry Point Invariants

**Entry Point Properties**:
- Entry point exists if and only if there are nodes in the index
- Entry point is always a valid node (exists in `nodes` map)
- Entry point level equals `maxLevel`
- Entry point is reachable (has incoming connections or is entry)

**Selection Algorithm**:
```go
func selectNewEntryPoint() *HSNWNode {
    var best *HSNWNode
    maxLevel := -1

    for _, node := range idx.nodes {
        if node.Level > maxLevel {
            maxLevel = node.Level
            best = node
        }
    }

    return best // May be nil if no nodes
}
```

### 4. Layer Connectivity

**Within Each Layer**:
- Graph should be connected (all nodes reachable from entry point via layer 0)
- Higher layers form sub-graphs with fewer nodes
- Each layer is a connected component (ideally)

**Verification**:
```go
func isConnected(idx *HSNWIndex, level int) bool {
    if idx.entryPoint == nil {
        return len(idx.nodes) == 0
    }

    visited := make(map[string]bool)
    queue := []string{idx.entryPoint.ID}

    // BFS at given level
    for len(queue) > 0 {
        id := queue[0]
        queue = queue[1:]

        if visited[id] {
            continue
        }
        visited[id] = true

        node := idx.nodes[id]
        if level < len(node.Neighbors) {
            for _, nid := range node.Neighbors[level] {
                if !visited[nid] {
                    queue = append(queue, nid)
                }
            }
        }
    }

    // Check if all nodes at this level were visited
    expectedCount := 0
    for _, node := range idx.nodes {
        if node.Level >= level {
            expectedCount++
        }
    }

    return len(visited) == expectedCount
}
```

## Topology Guarantees During Removal

### Step-by-Step Topology Preservation

Given a node `N` to delete with neighbors `{A, B, C}` at level `L`:

**Before Removal**:
```
    A --- N --- B
          |
          C
```

**Step 1: Clean Incoming Links**
```go
// Remove N from all other nodes' neighbor lists
for _, otherNode := range idx.nodes {
    for level := 0; level <= otherNode.Level; level++ {
        otherNode.Neighbors[level] = removeFromSlice(
            otherNode.Neighbors[level], N.ID)
    }
}
```

Result:
```
    A     N     B
          |
          C
```

**Step 2: Reconnect Neighbors**
```go
// Create connections between N's neighbors
for level := 0; level <= N.Level; level++ {
    reconnectNeighbors(N.Neighbors[level], level)
}
```

Result:
```
    A --- B
    |    / \
    C ---   (N deleted)
```

**Step 3: Delete Node**
```go
delete(idx.nodes, N.ID)
```

**Step 4: Update Entry Point (if necessary)**
```go
if idx.entryPoint.ID == N.ID {
    idx.entryPoint = selectNewEntryPoint()
    idx.maxLevel = idx.entryPoint.Level
}
```

### Reconnection Strategy

The `reconnectNeighbors()` function creates a mesh topology among deleted node's neighbors:

```go
func reconnectNeighbors(neighbors []string, level int) {
    // Create pairwise connections
    for i := 0; i < len(neighbors); i++ {
        for j := i+1; j < len(neighbors); j++ {
            node1 := idx.nodes[neighbors[i]]
            node2 := idx.nodes[neighbors[j]]

            // Add bidirectional link if not present and under limit
            if !contains(node1.Neighbors[level], neighbors[j]) &&
               len(node1.Neighbors[level]) < maxConnections(level) {
                node1.Neighbors[level] = append(
                    node1.Neighbors[level], neighbors[j])
            }
            if !contains(node2.Neighbors[level], neighbors[i]) &&
               len(node2.Neighbors[level]) < maxConnections(level) {
                node2.Neighbors[level] = append(
                    node2.Neighbors[level], neighbors[i])
            }
        }
    }
}
```

**Complexity**: O(M²) where M = number of neighbors

**Benefits**:
- Maintains graph connectivity
- Provides alternative paths
- Preserves search quality

**Trade-offs**:
- May exceed M temporarily (will be pruned on next insert)
- Creates denser local topology

## Topology After Bulk Operations

### Mass Deletion Effects

When removing many nodes (>30% of index):

1. **Graph Fragmentation**: Temporary disconnections may occur
2. **Layer Imbalance**: Higher layers may become sparse
3. **Entry Point Volatility**: Entry point may change frequently

**Mitigation**:
```go
// After bulk deletions
for _, id := range toDelete {
    idx.Remove(id)
}

// Restore optimal topology
idx.OptimizeIndex()
```

### Optimization Pass

`OptimizeIndex()` performs:

1. **Entry Point Optimization**: Select best entry point
2. **Dangling Reference Cleanup**: Remove invalid neighbor IDs
3. **Future**: Layer rebalancing

```go
func (idx *HSNWIndex) OptimizeIndex() error {
    idx.mu.Lock()
    defer idx.mu.Unlock()

    // 1. Find optimal entry point
    idx.optimizeEntryPoint()

    // 2. Clean up graph
    idx.cleanupDanglingReferences()

    return nil
}
```

## Diagnostic Tools

### Detecting Topology Violations

```go
diag := idx.Diagnostics()

// Check for issues
if diag.DanglingReferences > 0 {
    // Neighbors pointing to deleted nodes
    log.Printf("CRITICAL: %d dangling references", diag.DanglingReferences)
    idx.OptimizeIndex()
}

if diag.OrphanedNodes > 0 {
    // Nodes with no incoming connections (except entry point)
    log.Printf("WARNING: %d orphaned nodes", diag.OrphanedNodes)
}

if !diag.EntryPointValid {
    // Entry point doesn't exist or is invalid
    log.Printf("CRITICAL: Invalid entry point")
    idx.OptimizeIndex()
}
```

### Graph Health Metrics

```go
// Average degree (connectivity)
if diag.AvgDegree < expectedMinDegree {
    log.Printf("WARNING: Low connectivity: %.2f", diag.AvgDegree)
}

// Node distribution across levels
stats := idx.Stats()
if stats.MaxLevel > 16 {
    log.Printf("WARNING: Very deep graph: level %d", stats.MaxLevel)
}
```

## Edge Cases and Handling

### Case 1: Removing Last Node
```go
idx.Add("node1", vector)
idx.Remove("node1")

// Results:
// - idx.nodes = empty
// - idx.entryPoint = nil
// - idx.maxLevel = 0
```

### Case 2: Removing Entry Point
```go
idx.Add("node1", vector1) // Level 3 - becomes entry point
idx.Add("node2", vector2) // Level 1
idx.Remove("node1")

// Results:
// - idx.entryPoint = node2
// - idx.maxLevel = 1
```

### Case 3: Removing Hub Node
```go
// Node with many connections
nodeX.Neighbors[0] = [A, B, C, D, E, F, G, H]
idx.Remove("nodeX")

// Results:
// - All neighbors become interconnected (mesh)
// - May temporarily exceed M connections
// - Next insert will prune to M
```

### Case 4: Concurrent Removal and Search
```go
// Thread 1: Searching
results := idx.Search(query, k)

// Thread 2: Removing
idx.Remove("nodeY")

// Possible outcomes:
// 1. Search completes before removal: nodeY may be in results
// 2. Search reads neighbor list with nodeY, but node is deleted:
//    Search handles gracefully (node lookup returns nil)
// 3. Search starts after removal: nodeY never in results
```

## Topology Testing

### Connectivity Test
```go
func TestGraphConnectivity(t *testing.T) {
    idx := createIndex()

    // BFS from entry point at layer 0
    reachable := bfsReachable(idx, idx.entryPoint, 0)

    // All nodes should be reachable
    if len(reachable) != len(idx.nodes) {
        t.Errorf("Disconnected graph: %d reachable, %d total",
            len(reachable), len(idx.nodes))
    }
}
```

### Layer Integrity Test
```go
func TestLayerIntegrity(t *testing.T) {
    idx := createIndex()

    layerCounts := make(map[int]int)
    for _, node := range idx.nodes {
        for level := 0; level <= node.Level; level++ {
            layerCounts[level]++
        }
    }

    // Layer 0 must have all nodes
    if layerCounts[0] != len(idx.nodes) {
        t.Errorf("Layer 0 incomplete")
    }

    // Higher layers should have fewer nodes
    for level := 1; level <= idx.maxLevel; level++ {
        if layerCounts[level] > layerCounts[level-1] {
            t.Errorf("Layer %d has more nodes than layer %d",
                level, level-1)
        }
    }
}
```

### Dangling Reference Test
```go
func TestNoDanglingReferences(t *testing.T) {
    idx := createIndex()

    for _, node := range idx.nodes {
        for level := 0; level <= node.Level; level++ {
            for _, neighborID := range node.Neighbors[level] {
                if _, exists := idx.nodes[neighborID]; !exists {
                    t.Errorf("Dangling reference: %s -> %s at level %d",
                        node.ID, neighborID, level)
                }
            }
        }
    }
}
```

## Performance Impact

### Topology Maintenance Costs

| Operation | Time Complexity | Notes |
|-----------|----------------|-------|
| Remove | O(N*M) | Clean all neighbor lists |
| Reconnect | O(M²) | Connect deleted node's neighbors |
| Select Entry Point | O(N) | Find highest level node |
| Optimize | O(N*M) | Clean dangling references |
| Diagnostics | O(N*M) | Read-only scan |

Where:
- N = number of nodes in index
- M = max connections per node (typically 16-32)

### Memory Impact

- **Per Node**: ~(M * L * 8) bytes for neighbor lists
- **Total**: ~(N * M * L * 8) bytes
- **After Removal**: Memory is freed immediately

## Best Practices

1. **Batch Deletions**: Delete multiple vectors, then optimize once
2. **Monitor Health**: Check diagnostics periodically
3. **Optimize After Bulk Ops**: Run `OptimizeIndex()` after >10% deletions
4. **Entry Point Stability**: Avoid removing highest-level nodes frequently
5. **Concurrent Safety**: Use locks appropriately for concurrent access

## Conclusion

The topology maintenance in MagpieDB ensures that the HNSW graph remains valid, connected, and efficient even under heavy deletion workloads. The invariants are actively enforced and can be verified through diagnostic functions.
