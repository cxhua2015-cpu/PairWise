# balanceledger217

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top` 与 `Snapshot` 在读取时现排序，避免写路径为有序结构付出额外代价（账户数在控制面场景通常有限）。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节长度），不读取任何状态。
- 随后在写锁内构建候选事务：仅把被触及的账户复制到临时 map 中按输入顺序执行 Add/Set/Delete，revision 在局部计数器上分配。
- Add 在算术前检测 int64 溢出，结果与 Set 值都执行绝对值上限检查；账户容量上限只在批次末按最终净增减检查一次。
- 任一步失败直接返回，已提交状态（账户、revision、generation）完全不受影响，天然回滚；全部成功才一次性写回，非空批次 generation 恰好加一。

### 所有权
- `Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响账本内部状态。
- 所有公开方法通过 `sync.RWMutex` 保护：写（`Apply`）独占，读（`Top`/`Snapshot`）共享，可安全并发调用。

### 复杂度
- `Apply`：O(k·t)，k 为批内 op 数，t 为批内触及的不同账户数（`touched` 线性查找；t 通常很小）。
- `Top`：O(n log n)，n 为账户总数。
- `Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)，候选事务额外 O(t)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
