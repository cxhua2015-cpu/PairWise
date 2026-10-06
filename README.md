# metacatalog206

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 记录存储在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；另维护 `totalValue` 累计值，避免每次容量检查时遍历。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，保证确定性输出。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做完整结构校验（名称字符集/长度、Kind 合法、Delete 不带 Value、Value 长度上限），不读取任何状态。
- 校验通过后，在记录的**副本**上按输入顺序执行 Put/Delete：Put 分配连续递增的 revision，Delete 不分配；Delete 缺失键立即以 `ErrNotFound` 失败。
- 记录数与 Value 总字节容量只在批次末检查，超出返回 `ErrCapacity`。
- 任一步失败直接丢弃副本——状态、generation、revision 全部天然回滚；成功时一次性提交副本，非空批次 generation 恰好 +1。

### 所有权
- Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回深拷贝。
- 调用方对入参或返回切片的任何修改都不会影响内部状态，反之亦然。

### 并发
- 单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，所有公开方法可安全并发调用（`-race` 通过）。

### 复杂度
- `Apply`：时间 O(k + n)，k 为批内操作数、n 为当前记录数（克隆候选副本）；空间 O(n)。
- `Get`：O(1)（加上返回值拷贝 O(v)）。
- `Snapshot`：O(n log n)（排序），空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
