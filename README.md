# expirytable204

Read `SPEC.md` and implement the package.

## 实现说明

### 索引
表内以 `map[string]Entry` 作为主索引，键到条目 O(1) 定位。`Snapshot` 与 `Expire`
返回的条目按字典序排序，保证规范、确定的输出顺序。

### 候选事务
`Apply` 先在持锁状态下对整个批次做结构校验（键字符集与长度、kind 合法性、
非负时间），再检查单调时间。通过后克隆当前条目表为候选状态：先在候选上删除
`ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配单调递增
revision）。最终容量超限或任何错误发生时直接丢弃候选，淘汰、时间与 revision
随之整体回滚；只有全部成功才一次性提交并令 generation 加一（空批次不变）。
`Expire` 使用相同的闭区间边界 `ExpiresAt <= now`。

### 所有权与并发
所有公开方法由单把互斥锁保护，可并发调用。`Snapshot` 与 `Expire` 返回的切片
均为新分配的副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。

### 复杂度
设 n 为条目数、m 为批次操作数：Put/Touch/Delete 单操作 O(1)；`Apply` 为
O(n + m)（候选克隆与淘汰扫描）；`Expire` 为 O(n + k log k)（k 为到期条目数）；
`Snapshot` 为 O(n log n)（排序）。空间 O(n)。
