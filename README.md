# balanceledger252

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚；`Top` 按值降序、名称升序，`Snapshot` 按名称排序。

## 架构与多文件联动

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot` 与类型、错误值定义。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 仅做结构校验（kind、名称字符集与
  字节上限、Add 非零 delta、绝对值上限），不读取账户状态。`Apply` 复用同一入口，
  保证预检与事务共享一致结构语义。
- `stats.go` — 线性一致统计：`Stats` 在读锁内一次性采样 generation、next revision
  与账户数，与并发事务状态一致。
- `clone.go` — 深拷贝：`Clone` 复制全部账户与逻辑时钟（generation、revision），
  克隆体与原账本完全隔离，互不影响。

## 索引与所有权

- 状态存放在 `map[string]Account`（按名称的哈希索引），配 `sync.RWMutex`：
  写事务持写锁，`Top`/`Snapshot`/`Stats`/`Clone` 持读锁并发执行。
- 所有返回的切片均为新建副本，调用方修改不会影响内部状态（所有权隔离）。

## 候选事务（candidate transaction）

`Apply` 先通过 `ValidateBatch` 做完整结构预检，再在账户表的私有副本上按序执行
操作：算术前检测 int64 溢出并执行绝对值上限，批次末才检查最终账户容量。
任一步失败直接丢弃副本（天然回滚，无副作用）；成功时一次性换入副本，
generation 仅增一次，revision 按 Add/Set 连续分配。

## 复杂度

- `ValidateBatch`：O(B)，B 为批内操作数。
- `Apply`：O(A + B)，A 为当前账户数（复制候选表），B 为批内操作数。
- `Top(n)`：O(A log A) 排序后取前 n。
- `Snapshot`：O(A log A)（按名称排序）。
- `Stats`：O(1)；`Clone`：O(A)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
