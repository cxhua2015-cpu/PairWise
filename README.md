# topologygraph378

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- `nodes map[string]struct{}`：节点存在性 O(1) 查询。
- `edges map[Edge]struct{}`：边（`From`,`To`）存在性 O(1) 查询。
- `out map[string]map[string]struct{}`：出边邻接表，用于环检测与 `Reachable` 的 DFS。入边不单独建索引；`DeleteNode` 级联删除时遍历 `edges` 过滤关联边。

## 候选事务（candidate transaction）

`Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集/长度、无多余字段），不读状态；随后在持写锁的情况下把 `nodes`/`edges`/`out` 深拷贝为候选状态，按顺序应用全部操作。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCycle`）或批次末容量（`MaxNodes`/`MaxEdges`）超限（`ErrCapacity`）时直接丢弃候选状态，已提交状态与 `generation` 完全不变——天然整体回滚。全部成功才用候选状态替换提交状态，并将 `generation` 恰好加一；空批次不增加。

## 所有权与并发

- 所有公开方法并发安全：`Apply` 取 `sync.Mutex` 写锁，`Reachable`/`Snapshot` 取读锁，因此 `Reachable` 总是基于某一已提交 generation 的一致快照。
- `Snapshot` 返回新建切片（节点按字典序、边按 `(From,To)` 稳定排序），与内部 map 完全隔离；调用方修改返回值不影响图。
- 返回的 `Result`/`Snapshot` 均为值语义，无共享内部指针。

## 复杂度

设批次操作数 `k`，节点数 `n`，边数 `m`：

- `Apply`：结构校验 O(k·L)（L 为名称长度）；候选拷贝 O(n+m)；`AddEdge` 环检测为一次 DFS，O(n+m)；容量检查 O(1)。整体 O(k·(n+m)) 上界。
- `Reachable`：一次 DFS，O(n+m)。
- `Snapshot`：O(n log n + m log m) 排序。
- 空间：O(n+m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
