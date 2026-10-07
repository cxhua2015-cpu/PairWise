# metacatalog346

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`（按名称 O(1) 定位），另维护 `totalValue` 计数器用于 O(1) 的总字节容量判定。
- `Snapshot` 与 `Result.Changed` 不维护有序结构，而是按需对键名排序，避免为每次写入支付有序索引成本。

### 候选事务（candidate transaction）
- `Apply` 分三个阶段：先对整个批次做完整结构校验（不读任何状态）；再在互斥锁内把当前记录复制到候选 map 上按输入顺序执行 Put/Delete；最后仅在批次末检查记录数与 Value 总字节容量。
- 任何失败（`ErrInvalidInput` / `ErrNotFound` / `ErrCapacity`）都直接丢弃候选 map，已提交状态、generation 与 revision 完全不变，天然实现回滚。
- Put 在候选上分配连续 revision，Delete 不分配；非空成功批次 generation 只加一，空批次不变。

### 所有权
- Put 时拷贝调用方传入的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 均为深拷贝，返回切片与内部状态完全隔离，调用方后续修改互不影响。

### 并发
- 所有公开方法通过单个 `sync.Mutex` 串行化状态访问，可安全并发调用。

### 复杂度
- `Apply`：O(n + m log m)，n 为批次操作数，m 为触及的不同名称数（另加 O(记录数) 的候选复制）。
- `Get`：O(1)（不计返回值拷贝）。
- `Snapshot`：O(k log k)，k 为当前记录数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
