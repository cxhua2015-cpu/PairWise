# resourcelease189

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约键，按键 O(1) 定位。
- 不维护按过期时间的辅助索引；淘汰（Apply 候选阶段与 `Expire`）为全表扫描，
  对 N 个条目为 O(N)。`Snapshot` 返回按键排序的副本，为 O(N log N)。

## 候选事务

`Apply` 采用 copy-on-write 候选状态：先在整表副本上删除 `ExpiresAt <= Now` 的条目，
再顺序执行 Put/Touch/Delete（Put/Touch 各分配一个递增 revision），最后做容量检查。
任何一步失败（结构校验、时间回退、键不存在、最终容量超限）都会整体回滚——
淘汰、时间、revision、generation 均不生效；只有全部成功时才用候选状态原子替换现状。
非空成功批次 generation 恰好加一；空批次只推进时间，generation 不变。

## 所有权

- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由单把互斥锁保护，可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响内部状态；
  传入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。

## 复杂度

- `Apply`：O(N + M)，N 为条目数（候选复制与淘汰），M 为批内操作数。
- `Expire`：O(N + K log K)，K 为过期条目数（结果按键排序）。
- `Snapshot`：O(N log N)（排序副本）。
- 空间：O(N)。
