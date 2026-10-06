# metacatalog231

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete；Put 分配连续 revision，Delete 不分配；失败整体回滚状态、
generation 与 revision。公开契约见 `SPEC.md` 与 `metacatalog231/contract_test.go`。

## 多文件架构

实现刻意拆分为四个联动组件，共享同一套结构语义：

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot` 与存储表示。
- `validation.go` — 无副作用批次预检：`validateName`/`validateBatch`/`ValidateBatch`。
  `Apply` 与 `ValidateBatch` 调用同一 `validateBatch`，保证结构校验语义完全一致；
  预检不读取、不修改任何状态，可在锁外完成。
- `stats.go` — 线性一致统计：`Stats` 在读锁下汇总 generation、nextRevision、
  记录数与 Value 总字节，与并发事务状态保持一致。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 复制全部记录与逻辑时钟
  （generation/nextRevision），克隆体与原 Store 完全独立。

## 索引

- 主索引为 `map[string]Record`，按名称 O(1) 定位。
- `Snapshot`/`Apply` 的 `Changed` 在返回前按名称排序，保证确定性输出。
- 逻辑时钟：`generation` 仅在非空成功批次 +1；`nextRevision` 从 1 开始，
  每个 Put 分配一个连续 revision，Delete 不分配。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构预检**：对整个批次做完整结构校验（kind、名称字符集与长度、
   Put 值非空且不超上限、Delete 值必须为 nil），先于任何状态读取；
   未知 kind 或多余字段（Delete 携带 Value）返回 `ErrInvalidInput`。
2. **候选执行**：在写锁内把记录表复制为候选副本，按输入顺序在副本上应用
   全部操作（Delete 缺失记录返回 `ErrNotFound`）；仅在**批次末**检查最终
   记录数与 Value 总字节容量（`ErrCapacity`）。任一失败直接丢弃候选副本，
   状态、generation、revision 全部不变；成功则原子换入并推进时钟。

## 所有权

- 入参 Value 在写入前复制，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Apply` 的返回记录均为深拷贝，修改返回值不影响内部状态。
- `Clone` 深拷贝所有 Value，两个 Store 互不可见对方的后续写入。
- 返回的切片与内部状态完全隔离，无共享底层数组。

## 并发

所有公开方法可并发调用：写路径（`Apply`）持独占锁，读路径
（`Get`/`Snapshot`/`Stats`/`Clone`）持读锁；`ValidateBatch` 无状态访问。
`Stats`/`Clone`/`Snapshot` 因此在某一线性化点上观察一致的状态。

## 复杂度

- `Apply`：O(R + B + C log C)，R 为现有记录数（候选复制），B 为批次数，
  C 为变更名数（排序）。
- `Get`：O(1) 均摊（外加一次 Value 拷贝）。
- `Snapshot`/`Clone`：O(R log R) / O(R)，含深拷贝。
- `Stats`：O(R)。`ValidateBatch`：O(B)，无锁、无副作用。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
