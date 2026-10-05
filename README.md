# controlgraph158

并发安全的内存型“控制面依赖图 158”（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- `nodes map[string]struct{}`：节点存在性 O(1)。
- `edges map[Edge]struct{}`：边存在性 O(1)，`Edge{From,To}` 为可比较键。
- `out / in map[string]map[string]struct{}`：出/入邻接表。`out` 供环检测与 `Reachable` 的 DFS；`in` 与 `out` 一起让 `DeleteNode` 级联删边只触碰关联边。

### 候选事务（candidate transaction）
`Apply` 先做整批结构校验（kind、名称字符集/长度、多余字段），不读任何状态；
随后在写锁内把四张索引浅拷贝为 candidate，按序应用全部 Op（存在性/环检测都在
candidate 上执行），最后才检查最终节点/边容量。任一失败直接丢弃 candidate，
原图零改动，实现整体回滚；成功则一次性指针替换提交，非空成功批次 `generation` 恰好 +1。

### 所有权与并发
- 所有公开方法经 `sync.RWMutex` 保护：`Apply` 取写锁，`Reachable`/`Snapshot` 取读锁，
  因此 `Reachable` 总是基于某一已提交批次的一致快照。
- `Snapshot` 重建并排序切片（节点按字典序、边按 `(From,To)`），返回的切片与内部
  map 完全隔离，调用方修改不影响图；空批次返回当前 generation，不推进版本。

### 复杂度
- `AddNode`/`DeleteEdge`：O(1)；`DeleteNode`：O(关联边数)。
- `AddEdge`：环检测 DFS，O(V+E)（仅 candidate 内）。
- `Apply`：另加 O(V+E) 的 candidate 拷贝；容量检查 O(1)。
- `Reachable`：O(V+E)；`Snapshot`：O(V log V + E log E)。

## 验证
`go test ./...`、`go test -race ./...`、`go run ./cmd/demo` 全部通过。
