# balanceledger282

并发安全的内存型余额账本，仅依赖 Go 标准库（Go 1.22+）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，失败整体回滚。

## 多文件架构

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot`，持有互斥锁与状态。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 只读取 `Options`，不触碰账本状态；
  `Apply` 在加锁前复用同一套结构语义（kind 合法、名称字符集与字节上限、Delta/Value 绝对值上限）。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回 generation、nextRevision 与账户数快照。
- `clone.go` — 所有权安全的深拷贝：`Clone` 在读锁下复制全部账户与逻辑时钟
  （generation/nextRevision），副本与原账本完全隔离。

## 索引与数据结构

- 账户存储为 `map[string]Account`（按名称 O(1) 存取），无额外有序索引。
- `Top` 每次调用对账户做全量排序（值降序、名称升序），`Snapshot` 按名称排序；
  返回切片均为新建副本，与内部状态隔离。

## 候选事务与回滚

`Apply` 在写锁内先将账户表浅拷贝为候选 map，按顺序在候选上执行全部操作
（Add 前检测 int64 溢出并执行绝对值上限，Delete 检查存在性），批次末才校验最终账户容量。
任一失败直接丢弃候选，原状态、generation 与 revision 时钟完全不变；成功时一次性提交，
非空批次 generation 只加一，空批次不改变任何时钟。

## 所有权与并发

- 所有公开方法可并发调用：写路径持 `sync.Mutex` 写锁，读路径（`Top`/`Snapshot`/`Stats`/`Clone`）
  持读锁；`ValidateBatch` 纯函数无锁。
- 所有返回的切片与结构均为拷贝，调用方修改不会影响账本；`Clone` 产出的账本独立演进。

## 复杂度

- `Apply`：O(n + a)，n 为批次操作数，a 为当前账户数（候选拷贝）。
- `Top`：O(a log a)；`Snapshot`：O(a log a)；`Stats`：O(1)；`Clone`：O(a)。
- `ValidateBatch`：O(n · L)，L 为名称长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
