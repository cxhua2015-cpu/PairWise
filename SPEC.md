# DAG store specification

`New` requires positive `MaxNodes`, `MaxEdges`, `MaxNameBytes`, `MaxPayloadBytes`, and `MaxTotalPayloadBytes`; `MaxPayloadBytes` may not exceed `MaxTotalPayloadBytes`. Names are nonempty, bounded by `MaxNameBytes`, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` first structurally validates every operation in input order without reading state. AddNode and UpdateNode require only valid Name and non-nil Payload no larger than `MaxPayloadBytes`. DeleteNode requires only valid Name and nil Payload. AddEdge and RemoveEdge require empty Name, nil Payload, two distinct valid endpoints From and To. Unknown kinds or malformed fields return `ErrInvalidInput`.

Operations then execute sequentially on an isolated candidate. AddNode requires a missing name; UpdateNode and DeleteNode require an existing node. DeleteNode requires no incoming or outgoing edges at that point. AddEdge requires both nodes, an absent edge, and must not form a directed cycle. RemoveEdge requires an existing edge. Conflicts return `ErrConflict`, missing objects return `ErrNotFound`, and a cycle returns `ErrCycle`. Every successful operation consumes one consecutive revision starting at 1; nodes and edges store the revision of their latest creation/update. A removed object consumes a revision but does not survive in `ChangedNodes` or `ChangedEdges`.

Final node count, edge count, and total live Payload bytes are checked only after all operations. Any failure rolls back nodes, both edge indexes, generation, and revision allocation. A successful nonempty batch increments generation once; an empty batch does not. `Result.Revision` is the latest allocated revision. Changed surviving nodes are sorted by Name and changed surviving edges by From then To, without duplicates.

`Reachable(from,to)` validates both names and requires both nodes. A node is reachable from itself. `Topological()` returns the lexicographically smallest valid topological order by repeatedly selecting the smallest zero-indegree node. `Snapshot` returns nodes by Name and edges by From then To. All Payload slices are ownership-isolated and all methods are concurrency-safe.
