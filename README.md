# expirytable324

Read `SPEC.md` and implement the package.

## 实现说明

### 索引
表内以 `map[string]Entry` 作为主索引，键到条目 O(1) 定位。到期淘汰采用全量扫描（`ExpiresAt <= now` 闭区间），未维护额外的时间堆；`Snapshot` 与 `Expire` 返回的条目按字典序排序以保证确定性。

### 候选事务
`Apply` 先在持锁前对整批 Op 做纯结构校验（kind、键字符集与字节上限），再在锁内检查单调时间。随后在候选状态（条目表的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision。最终容量校验或任何中途错误都会丢弃候选状态，淘汰、时间与 revision 一并回滚；只有全部成功才提交并令 generation 恰好加一（空批次不变）。

### 所有权与并发
所有公开方法经由单一 `sync.Mutex` 串行化，可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方对返回值（含 `Entry`）的修改不会影响内部状态，内部状态也不会在返回后被改写。

### 复杂度
设 n 为条目数、b 为批次大小：Put/Touch/Delete 单操作 O(1)；`Apply` 为 O(n + b)（候选复制 + 顺序执行）；`Expire` 与 `Snapshot` 为 O(n log n)（含排序）；`New` 为 O(1)。空间 O(n)。
