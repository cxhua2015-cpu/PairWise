# resourcecatalog111

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- `Store` 另维护 `totalBytes`（Value 总字节）、`generation`、`revision` 三个标量，
  随批次原子更新，避免容量检查时的全表扫描。
- 单把 `sync.RWMutex`：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，读操作可并行。

### 候选事务（candidate transaction）
- `Apply` 先在**不加锁**的情况下对全部 Ops 做完整结构校验（kind、名称字符集与长度、
  Value 长度），任何错误直接返回 `ErrInvalidInput`，此阶段不读取任何状态。
- 校验通过后取写锁，把当前 `records` 克隆为候选 map，按输入顺序在其上执行
  Put/Delete：Put 递增并分配连续 revision，Delete 不分配；Delete 缺失记录即返回
  `ErrNotFound`。
- 记录数与 Value 总字节上限只在批次末对候选状态检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选 map——`records`、`totalBytes`、`generation`、`revision`
  均未被修改，天然完成回滚；成功则整体换入候选状态，非空批次 `generation` 恰好 +1。

### 所有权
- Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value
  均为深拷贝，返回切片与内部状态完全隔离，调用方修改互不影响。

### 复杂度
- `Apply`：结构校验 O(B)，候选克隆 O(N)，执行 O(B)，合计 O(N + B)，
  B 为批内操作数、N 为当前记录数。
- `Get`：O(1)（不计返回值拷贝）。
- `Snapshot`：O(N log N)（按名称排序）。
- 空间：O(N) 外加 `Apply` 期间的候选副本 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
