# resourceledger192

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，失败整体回滚。

## 设计说明

- **索引**：账户状态存放在唯一的 `map[string]Account` 主索引中（按名称 O(1) 定位）。
  不维护有序辅助索引；`Top` 与 `Snapshot` 在读取时对当前账户集复制并排序，
  以换取写入路径的极简与无锁序反转风险。
- **候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰状态；
  随后在写锁内把主索引浅拷贝为候选映射（`Account` 为值类型，拷贝即隔离），
  所有 Add/Set/Delete 与溢出、绝对值上限检查都在候选上执行，最终账户容量仅在
  批次末检查。任一步失败直接返回，主索引、generation、revision 均不变，实现整体回滚；
  全部成功才用候选替换主索引并提交。
- **所有权**：`Account` 是纯值类型，返回的 `Result.Changed`、`Top`、`Snapshot.Accounts`
  均为新建切片，调用方修改不会影响账本内部状态；内部也从不保留对外切片的引用。
- **并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 取写锁，`Top`/`Snapshot` 取读锁，
  所有公开方法可安全并发调用（`-race` 验证）。
- **复杂度**：设批次含 k 个 op、账本共 n 个账户。`Apply` 为 O(n + k)（候选拷贝 O(n)，
  每个 op O(1)）；`Top` 与 `Snapshot` 为 O(n log n)；空间 O(n + k)。

## 语义要点

- `Add`/`Set` 各分配一个连续 revision，`Delete` 不分配；非空成功批次 generation 只加一。
- int64 溢出在算术前检测；`|value| <= MaxAbsValue`（`math.MinInt64` 恒非法）。
- `Top` 按值降序、名称升序；`Snapshot` 按名称升序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
