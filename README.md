# workerlease

并发安全的内存型工作节点租约表，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

### 索引

租约条目存储在以 key 为键的哈希表（`map[string]Entry`）中，Put/Touch/Delete
与过期扫描均为哈希定位；过期判断直接扫描候选 map（条目数受 `MaxEntries` 上限约束），
不维护额外的堆或时间索引，以换取实现简单与回滚方便。`Snapshot` 与 `Expire`
返回的条目按 key 字典序排列（规范顺序），保证输出确定性。

### 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、key 字符集与长度、非负 ExpiresAt），
   再检查时间单调性，任一失败直接返回，不触碰状态。
2. 将当前条目复制到候选 map，先在候选上淘汰 `ExpiresAt <= Now` 的条目，
   再按顺序执行 Put/Touch/Delete；Put/Touch 从候选的 revision 计数器分配版本号。
3. 任何错误（未知 key、最终容量超限）都会丢弃候选，淘汰、时间与 revision
   一并回滚，表保持应用批次前的状态。
4. 全部成功才一次性提交：替换条目 map、推进 `now`、generation 恰好加一、
   提交 revision 计数器。空批次为完全无操作（generation 与时间均不变）。

`Expire(now)` 使用与 `Apply` 相同的闭区间边界（`ExpiresAt <= now`）与单调时间检查，
直接在当前状态上删除并返回被淘汰的条目。

### 所有权与并发

所有公开方法可并发调用：`Table` 内部以单个 `sync.Mutex` 串行化全部读写，
批次内操作不会与其他调用交错。`Snapshot` 与 `Expire` 返回的切片均为新分配的
副本，调用方修改返回结果不会影响表内状态（所有权随返回值转移）。

### 复杂度

设批次含 B 个操作、表内 N 个条目（N ≤ MaxEntries）：

- `Apply`：O(N + B)，候选复制与过期扫描 O(N)，每个操作 O(1)。
- `Expire`：O(N + E log E)，E 为被淘汰条目数（排序输出）。
- `Snapshot`：O(N log N)（排序输出）。
- 空间：O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
