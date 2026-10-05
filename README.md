# resourceledger082

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top` 与 `Snapshot` 在读取时拷贝全部账户并排序，避免写路径上的额外维护成本，同时保证返回切片与内部状态完全隔离。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做纯结构校验（kind 合法、名称字符集与字节上限），不触碰任何状态。
- 随后在写锁内把当前账户表克隆为候选映射，所有 Add/Set/Delete 都作用于候选；任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，已提交状态零改动，实现整体回滚。
- Add 在算术前用 `math.MaxInt64-d` / `math.MinInt64-d` 边界比较检测 int64 溢出，再执行 `MaxAbsValue` 绝对值上限；账户容量上限仅在批次末尾对候选统一检查。
- 提交时一次性替换映射、推进 `nextRev`，非空成功批次 `generation` 恰好加一。

### 所有权
- `Ledger` 内部状态仅由包内代码持有；`Top`、`Snapshot`、`Result.Changed` 返回的切片均为新建拷贝，调用方修改不影响账本，后续批次也不会改写已返回的数据。
- 并发控制使用一把 `sync.RWMutex`：`Apply` 取写锁，`Top`/`Snapshot` 取读锁，所有公开方法可安全并发调用（`-race` 验证）。

### 复杂度
- `Apply`：O(k·n) 克隆 + O(k) 执行（k 为批内操作数，n 为账户数）；克隆保证回滚零残留。
- `Top`：O(n log n) 排序后取前 m 个；`Snapshot`：O(n log n) 按名称排序。
- 空间：O(n)，候选事务期间临时 O(n)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
