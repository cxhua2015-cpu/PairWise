# costledger

并发安全的内存型成本计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`（名称 → 账户），Add/Set/Delete 均为 O(1) 均摊。
- 不维护有序索引：`Top` 与 `Snapshot` 在调用时按需排序，避免写路径为读路径付费。

**候选事务（candidate transaction）**
- `Apply` 先对全部 op 做纯结构校验（kind、名称字符集与字节上限），不触碰状态。
- 然后在 `maps.Clone` 出的候选 map 上按输入顺序执行：Add/Set 分配连续 revision，
  算术前检测 int64 溢出并执行绝对值上限（`ErrValue`），Delete 缺失返回 `ErrNotFound`。
- 账户容量仅在批次末对候选结果检查（`ErrCapacity`）。
- 任一步失败直接丢弃候选，主状态（含 generation 与 revision 计数器）零副作用，天然整体回滚；
  成功时一次性交换候选 map 并提交计数器，非空批次 generation 只增一次。

**所有权与并发**
- 所有公开方法共用一把 `sync.Mutex`；`Ledger` 独占内部 map，提交后旧候选即被替换，无共享可写状态。
- `Top`/`Snapshot`/`Result.Changed` 返回的切片与 `Account` 值均为新建拷贝，调用方修改不影响内部状态。

**复杂度**（n = 账户数，k = 批内 op 数，m = Top 请求数）
- `Apply`：O(k) 校验与执行 + O(n) 候选克隆 + O(k log k) 变更排序。
- `Top`：O(n log n) 排序，返回前 m 项（值降序、名称升序）。
- `Snapshot`：O(n log n)，按名称排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
