# dedupcache

并发安全、显式时间驱动的内存幂等结果缓存（Go 1.22+，仅标准库）。读取 `SPEC.md` 获取完整规范。

## 事务语义

`Apply` 是原子的：先对整个批次做结构校验（键/令牌字符集与长度、Value 非空且不超限、`ExpiresAt > Now`、Delete 的字段约束、未知 Kind），再检查时间单调性（`Now >= 当前时间`）。任何失败整体回滚——不淘汰、不执行、不推进时间、revision 或 generation。容量检查（条目数与 Value 总字节）在候选状态全部操作执行完后进行，超限同样整体回滚。

## 到期边界

记录存活当且仅当 `ExpiresAt > Now`。每个批次（以及 `Get`）先全局淘汰 `ExpiresAt <= Now` 的记录，再执行操作；因此到期键可在同一批次内以新令牌重建。淘汰触发一次 generation 递增。

## 重放与冲突

同键同令牌的 Put 是重放：返回已存记录，提交的 Value/ExpiresAt 不生效，也不分配 revision。同键不同令牌返回 `ErrConflict`。Delete 要求记录当前存在（否则 `ErrNotFound`），删除不分配 revision。

## 所有权

所有字节输入在写入前深拷贝，所有输出（Outcome、Get、Snapshot）均为深拷贝；调用方修改自己的切片不会影响缓存，反之亦然。`Snapshot` 按 Key 排序返回记录。

## 并发与复杂度

所有公开方法通过单一互斥锁串行化，可安全并发调用。批次校验与执行均为 O(批次大小 + 记录数)；`Snapshot` 为 O(n log n)（排序）。
