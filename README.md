# metacatalog221

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`Store`、`New`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共享同一套
  `validName` / `validateOp` 规则（名称字符集与长度、kind 合法性、Put 的 Value 上限、
  Delete 必须携带 nil Value）。预检只读 `Options`，绝不触碰状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下汇总 generation、nextRevision、
  记录数与 Value 总字节，与并发事务状态一致。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 复制全部记录与逻辑时钟
  （generation / nextRevision），新旧 Store 互不影响。

## 索引

主索引为 `map[string]entry`（名称 → 值 + revision），点查 O(1)。
有序视图（`Snapshot`、`Result.Changed`）在读取时按名称排序，不维护额外有序结构。

## 候选事务

`Apply` 分两阶段：

1. 完整结构校验（`ValidateBatch`），先于任何状态读取；失败返回 `ErrInvalidInput`。
2. 在写锁内把当前记录复制到候选 map，按输入顺序执行 Put/Delete：
   Put 从 `nextRevision` 起分配连续 revision，Delete 不分配、缺失返回 `ErrNotFound`。
   记录数与 Value 总字节容量只在批次末检查（`ErrCapacity`）。
   任何失败直接丢弃候选，状态、generation、revision 全部回滚；
   成功则整体换入候选，非空批次 generation 恰好加一。

## 所有权

写入时复制调用方的 Value；`Get`/`Snapshot`/`Result.Changed` 返回深拷贝，
`Clone` 生成完全独立的副本。返回的切片与内部状态完全隔离。

## 复杂度

- `Get`：O(1)；`Apply`：O(N + R)，N 为批次数、R 为当前记录数（候选复制）；
- `Snapshot` / `Stats` / `Clone`：O(R log R)（排序）/ O(R) / O(R + V)，V 为 Value 总字节。

所有公开方法均可并发调用：写操作互斥，读操作共享读锁。
