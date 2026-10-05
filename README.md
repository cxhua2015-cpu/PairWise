# resourcecatalog126

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护 `totalValue`（Value 总字节数）与 `revision`（已分配的最大 revision）两个标量，
  避免每次容量检查时遍历全表。
- `Snapshot` 与 `Result.Changed` 在返回前对名称排序，排序成本为 O(k log k)，k 为涉及记录数。

### 候选事务（candidate transaction）
- `Apply` 先做**整批结构校验**（kind 合法、名称字符集/长度、Value 长度、Delete 不带 Value），
  任何失败在读取状态前返回 `ErrInvalidInput`。
- 校验通过后克隆当前 map 得到候选状态，按输入顺序应用 Put/Delete：
  Put 分配连续 revision，Delete 不分配；Delete 缺失记录返回 `ErrNotFound`。
- 记录数与 Value 总字节容量**只在批次末**检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选状态，`generation`/`revision`/记录全部不变（天然回滚）；
  成功才整体提交，非空批次 `generation` 恰好 +1。

### 所有权
- Put 时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value
  均为新分配的副本，返回切片与内部状态完全隔离，调用方修改互不影响。

### 并发与复杂度
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用。
- 设批次含 n 个 op、表中 m 条记录：
  - `Apply`：克隆 O(m) + 应用 O(n) + Changed 排序 O(n log n)。
  - `Get`：O(1)（外加返回值拷贝 O(|Value|)）。
  - `Snapshot`：O(m log m)。
- 空间：O(m) 主状态 + Apply 期间 O(m) 候选副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
