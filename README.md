# topologygraph338

并发安全的内存型有向控制拓扑图（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

- **索引**：图状态由两个哈希索引承载——`nodes map[string]struct{}` 与
  `edges map[Edge]struct{}`。节点存在性、边存在性均为 O(1) 查询；
  关联边删除与可达性遍历通过扫描边索引完成。
- **候选事务**：`Apply` 先对全部操作做纯结构校验（kind、名称字符集与字节上限、
  多余字段），不读取任何状态；随后把当前节点/边索引复制为候选副本，
  在副本上顺序执行操作（存在性检查、删节点级联删边、加边前以 DFS 检测
  `To ⇢ From` 路径以阻止有向环），批次末尾统一检查最终节点/边容量。
  任一步失败直接丢弃候选副本，实现整体回滚；全部成功才一次性替换内部索引，
  且非空成功批次只将 `generation` 递增一次，空批次不变。
- **所有权**：`Graph` 内部状态绝不外泄。`Snapshot` 返回新建切片（节点按字典序、
  边按 `(From, To)` 稳定排序），`Result`/`Snapshot` 均为值语义，调用方对返回
  切片的修改不影响图。`Reachable` 在 `RWMutex` 读锁内基于当前一致快照计算。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 取写锁，
  `Reachable`/`Snapshot` 取读锁，可并发执行。

## 复杂度

设 N 为节点数、E 为边数、K 为批次操作数：

- `Apply`：结构校验 O(K·L)（L 为名称长度）；候选复制 O(N+E)；
  每个 AddEdge 的环检测 O(E)；DeleteNode 级联 O(E)；容量检查 O(1)。
  整体 O(N + E + K·E)，空间 O(N+E)。
- `Reachable`：O(E) 时间，O(N) 空间。
- `Snapshot`：O(N log N + E log E)（排序），O(N+E) 空间。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
