# metacatalog226

并发安全的内存型“元数据目录 226”，面向分布式控制面。Go 1.22+，仅依赖标准库。
公开契约与边界语义见 `SPEC.md` 与 `metacatalog226/contract_test.go`。

## 架构（多文件联动）

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与名称语法校验。
  `Apply` 在读取任何状态之前调用同一套校验，保证预检与事务共享完全一致的结构语义。
- `stats.go` — 线性一致统计：`Stats` 在读锁内一次性采样 generation、
  next revision、记录数与 Value 总字节，始终对应事务历史中的单一一致时点。
- `clone.go` — 所有权安全的深拷贝：`Clone` 复制全部记录与逻辑时钟
  （generation / next revision），克隆体与原 store 不共享任何内存。

## 索引

记录存放在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；另维护
`totalValue` 运行计数，使容量检查与 `Stats` 无需遍历。`Snapshot`/`Changed`
在读取时按名称排序输出。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **预检**：完整结构校验（kind、名称语法、Put/Delete 的 Value 约束），
   不读取任何状态。
2. **候选执行**：在写锁内把整批 Put/Delete 应用到按名缓冲的候选变更集上，
   Put 在候选时钟上分配连续 revision，Delete 不分配；删除不存在的记录
   立即以 `ErrNotFound` 失败。记录数与 Value 总字节容量只在批次末对候选
   最终态检查（`ErrCapacity`）。任何失败直接丢弃候选集——记录、
   generation、revision 全部不变；成功才一次性提交，非空批次
   generation 恰好加一。

## 所有权

所有进入 store 的 Value 在提交前拷贝；`Get`/`Snapshot`/`Result.Changed`
返回深拷贝，调用方对返回切片的修改不会泄漏进内部状态，反之亦然。
`Clone` 逐条拷贝记录值，克隆体后续事务与原 store 完全隔离。

## 复杂度

- `Apply`：O(k + c log c)，k 为批内 op 数，c 为批内涉及的不同名称数（排序 Changed）。
- `Get`：O(1) 均摊（加返回值拷贝 O(v)）。
- `Snapshot`：O(n log n)，n 为记录数（排序）。
- `Stats`：O(1)；`Clone`：O(n + V)，V 为 Value 总字节。
- 并发：读写互斥由 `sync.RWMutex` 保证，`Get`/`Snapshot`/`Stats`/`Clone` 走读锁。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
