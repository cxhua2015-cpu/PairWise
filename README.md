# expirytable344

并发安全的内存型“到期状态表”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

**索引**：表内只有一份主索引 `map[string]Entry`，键即条目键，值含 `ExpiresAt` 与单调递增的 `Revision`。到期扫描是对该 map 的一次线性过滤，不维护额外的堆或时间轮——容量上限通常较小，简单结构换来无指针别名、易于推理的事务语义。

**候选事务**：`Apply` 先在锁内对全部操作做结构校验（kind 合法、键为非空 ASCII 小写字母/数字/连字符/下划线且不超 `MaxKeyBytes`、`ExpiresAt >= 0`），再检查时间单调性（`Now` 不得回退）。通过后克隆当前 map 得到候选状态：先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配新 revision）。最终容量超限或任一步出错时直接丢弃候选，淘汰、时间与 revision 一并回滚；成功才整体提交，非空批次 generation 恰好加一，空批次不改变任何状态。`Result.Revision` 为本批次最后一次 Put/Touch 分配的 revision。

**所有权**：`Table` 内部状态绝不外借。`Expire` 与 `Snapshot` 返回的切片均为新分配的拷贝（按键排序，确定性输出），调用方修改不影响表。所有公开方法共用一把 `sync.Mutex`，可任意并发调用；`New` 校验容量与长度上限必须为正，否则返回 `ErrInvalidOptions`。

**复杂度**：`Apply` 为 O(n + m)，n 为当前条目数（克隆与淘汰扫描），m 为批内操作数；`Expire` 为 O(n)；`Snapshot` 为 O(n log n)（排序）；空间 O(n)。时间只进不退，`Expire` 与 `Apply` 使用同一闭区间边界 `ExpiresAt <= Now`。
