# resourceledger082

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本主体为 `map[string]Account`（按名称 O(1) 定位）。不维护有序索引；`Top`/`Snapshot` 在读锁内对当前账户做一次性排序，避免写路径维护堆/树的开销。

**候选事务**：`Apply` 分两阶段。先在无锁状态下对整批 Op 做结构校验（kind、名称字符集与字节上限）；随后持写锁，把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete，Add/Set 分配连续 revision。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，原状态零改动，实现整体回滚；仅在批次末校验最终账户数容量，因此允许批次中间临时超容量。溢出在算术前用 `math.MaxInt64/MinInt64` 边界比较检测，结果再做绝对值上限检查。非空成功批次 generation 恰好 +1，空批次不变。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，调用方修改不影响内部状态；内部 map 只在写锁下整体替换，读路径共享只读。

**并发**：单把 `sync.RWMutex`：写操作（`Apply`）持写锁，`Top`/`Snapshot` 持读锁可并行。

**复杂度**（N = 账户数，K = 批内 Op 数）：
- `Apply`：时间 O(N + K)（候选拷贝 + 顺序执行），空间 O(N + K)。
- `Top`：O(N log N)，取前 n 名（值降序、名称升序）。
- `Snapshot`：O(N log N)，按名称升序。
- `New`：O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
