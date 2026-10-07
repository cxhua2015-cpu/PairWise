# metacatalog321

并发安全的内存型元数据目录，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]entry`（名称 → `{value, revision}`），Put/Delete/Get 均为 O(1) 均摊查找。
- 另维护 `totalValue` 运行计数（当前所有 Value 的总字节数），避免每次容量检查时重新求和。
- `Snapshot` 与 `Result.Changed` 在返回前对键名收集并排序（O(n log n)），保证按名称有序输出。

### 候选事务（candidate transaction）
- `Apply` 先在无锁状态下对整批 Op 做完整结构校验（kind、名称字符集与长度、Value 长度），失败直接返回 `ErrInvalidInput`，不读取任何状态。
- 加写锁后，将当前 `records` 克隆为候选映射，按输入顺序在其上执行 Put/Delete：Put 分配连续递增的 revision，Delete 不分配；删除不存在的键返回即返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末尾检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选映射与临时 revision 计数，原始状态、generation、revision 完全不变（天然回滚）；全部成功才一次性换入候选映射并提交，非空批次 generation 恰好加一。

### 所有权
- 存入的 Value 在 Put 时深拷贝，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改返回值不会污染内部状态；返回切片与内部状态完全隔离。

### 并发
- 所有公开方法通过 `sync.RWMutex` 保护：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可安全并发调用。

### 复杂度
- `Apply`：O(k·n) 克隆候选映射 + O(k) 执行 + O(k log k) 排序 Changed（k 为批内 Op 数，n 为现存记录数）。
- `Get`：O(1) 均摊 + O(v) 拷贝（v 为 Value 长度）。
- `Snapshot`：O(n log n) 排序 + O(Σv) 深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
