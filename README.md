# schemaindex

并发安全的内存型“模式索引”，用于分布式控制面。仅依赖标准库，需 Go 1.22+。语义详见 `SPEC.md`。

## 索引结构

`Store` 内部以 `map[string]entry` 作为主索引，键为记录名，值保存深拷贝后的 Value 与其 revision；另维护 `totalBytes`（Value 总字节）、`generation` 与 `revision` 两个单调计数器。一把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，因此所有公开方法可并发调用。

## 候选事务

`Apply` 分三个阶段：

1. **结构校验**：在不读取任何状态的前提下校验整个批次（kind 合法、名称字符集与长度、Value 长度、Delete 不得携带 Value），任一失败返回 `ErrInvalidInput`。
2. **候选执行**：把当前索引复制为候选 map，按输入顺序在其上执行 Put/Delete。Put 分配连续 revision 并做 upsert，Delete 要求记录存在（否则 `ErrNotFound`）且不分配 revision。
3. **提交或回滚**：仅在批次末检查记录数与 Value 总字节容量（`ErrCapacity`）。任一阶段失败直接丢弃候选，已提交状态、generation、revision 均不变；成功时整体换入候选，非空批次 generation 恰好加一。

## 所有权

写入时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 与记录切片均为新分配的深拷贝，调用方可自由修改，不影响内部状态，反之亦然。

## 复杂度

设批次含 B 个操作、当前 N 条记录、Value 总字节 V：

- `Apply`：时间 O(N + B + C log C)（候选复制 + 顺序执行 + Changed 按名排序，C 为触及的名字数），额外空间 O(N + V)。
- `Get`：O(1) 查询加 O(|value|) 拷贝。
- `Snapshot`：O(N log N + V)。
- `New`：O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
