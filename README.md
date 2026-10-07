# balanceledger402

并发安全的内存型余额账本，仅依赖 Go 标准库。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，失败整体回滚。

## 架构与多文件联动

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot` 与共享状态。
- `validation.go` — 无副作用的结构预检 `ValidateBatch`；`Apply` 在读取任何
  状态之前调用同一套 `validateOp`/`validName`，保证预检与事务语义一致。
- `stats.go` — `Stats` 在同一把互斥锁下读取，返回线性一致的
  generation/nextRevision/账户数。
- `clone.go` — `Clone` 在锁内深拷贝全部账户与逻辑时钟（generation、
  nextRevision），返回完全独立所有权的账本。

## 索引

状态为 `map[string]Account`（按名称 O(1) 定位），外加 `generation` 与
`nextRevision` 两个逻辑时钟。`Top`/`Snapshot` 不维护有序索引，按需对账户
快照排序（分别为值降序+名称升序、名称升序），以换取写入路径 O(1)。

## 候选事务

`Apply` 先经 `ValidateBatch` 做完整结构校验（kind 合法、名称字符集与字节
上限、字段不得冗余、Add 的 Delta 非零），不触碰状态；随后在候选副本
（账户 map 的浅值拷贝）上按序重放：Add/Set 前检测 int64 溢出与绝对值上限
（`ErrValue`），Delete 缺失报 `ErrNotFound`，账户容量仅在批次末检查
（`ErrCapacity`）。任一步失败即丢弃候选，原状态零副作用；成功则整体换入
候选，非空批次 generation 恰好加一。

## 所有权

所有公开方法共用一把 `sync.Mutex`，可并发调用。`Result.Changed`、`Top`、
`Snapshot` 均返回新分配的切片与值类型 `Account`，与内部状态完全隔离；
`Clone` 复制 map 与计数器，克隆体与原账本互不影响。

## 复杂度

设 n 为账户数、k 为批次操作数：

- `Apply`：O(n + k)（候选拷贝 + 重放）
- `ValidateBatch`：O(k · 名称长度)，不读状态
- `Top`：O(n log n)；`Snapshot`：O(n log n)；`Stats`：O(1)
- `Clone`：O(n)
