# expirytable284

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 索引结构

表内状态由两部分组成：`entries` 哈希索引（key → `Entry`，O(1) 定位）与 `order` 插入序键列表（保证 `Snapshot`/`Expire` 输出确定性顺序）。`Options` 在 `New` 时校验（容量与长度上限必须为正），此后不可变，因此无锁读取安全；所有可变状态由单把互斥锁保护，公开方法均可并发调用。

## 候选事务

`Apply` 先执行与 `ValidateBatch` 共享的纯结构预检（不读状态），再在锁内检查单调时间。随后在候选状态（`state` 副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）。最终容量超限或任何错误直接丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才提交。非空成功批次 generation 恰好加一，空批次不变。`Expire` 使用同一闭区间边界（`ExpiresAt <= now`）。

## 所有权

`Snapshot`、`Expire` 返回的切片均为新建副本；`Clone` 深拷贝条目与逻辑时钟（generation、nextRevision、now），克隆体与原表完全隔离，互不影响。

## 复杂度

- `Apply`：O(n + e)，n 为批次操作数，e 为候选拷贝的条目数。
- `Expire` / `Snapshot` / `Clone`：O(e)。
- `Stats` / `ValidateBatch`：O(1) / O(批次大小)，均不修改状态。
