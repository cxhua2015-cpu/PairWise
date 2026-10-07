# metacatalog401

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete：Put 分配连续 revision，Delete 不分配；失败回滚全部状态、
generation 与 revision。详见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为四个相互联动的文件：

- `servicecatalog.go` — 核心事务引擎：`Store`、`New`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套
  结构语义（kind、名称字符集与长度、Value 规则），只校验、不读写状态。
- `stats.go` — 线性一致的状态统计：`Stats` 在读锁下汇总记录数与 Value 总字节。
- `clone.go` — 保留逻辑时钟（generation/nextRevision）的深拷贝，以及共享的
  `cloneBytes` 所有权辅助函数。

## 索引

主索引为 `map[string]Record`，按名称 O(1) 定位。`Snapshot` 与 `Result.Changed`
在返回前按名称排序，排序仅作用于临时切片，不改变索引结构。

## 候选事务（candidate transaction）

`Apply` 先在无锁阶段做完整结构预检（`ValidateBatch`），再在写锁内把当前记录
复制到候选 map 上按序重放整个批次。任何状态错误（`ErrNotFound`、批次末的
`ErrCapacity`）都会直接丢弃候选 map，原始状态、generation 和 revision 天然
保持不变，无需显式补偿。批次成功提交时才一次性换入候选 map、推进
nextRevision，且非空批次 generation 只加一。

## 所有权

所有进出边界的 `[]byte` 都经 `cloneBytes` 深拷贝：Put 时拷入，Get/Snapshot/
Clone 时拷出。调用方之后修改入参或返回值都不会影响目录内部状态，克隆体与
原 Store 完全隔离。返回的切片（`Changed`、`Records`）均为新建，与内部状态隔离。

## 并发与复杂度

- 所有公开方法可并发调用：写路径（`Apply`）持写锁，读路径（`Get`、
  `Snapshot`、`Stats`、`Clone`）持读锁，`ValidateBatch` 不访问状态。
- `Apply`：O(B + N)，B 为批次大小，N 为当前记录数（候选复制与批次末容量统计）。
- `Get`：O(V)，V 为 Value 字节数（深拷贝）。
- `Snapshot` / `Clone`：O(N·(log N + V̄))，排序加深拷贝。
- `Stats`：O(N)。`ValidateBatch`：O(B)，零状态访问。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
