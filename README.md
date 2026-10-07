# expirytable369

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：表内部使用 `map[string]Entry` 作为主索引，键即条目 Key，Put/Touch/Delete 均为 O(1) 均摊。未维护额外的堆或时间轮索引——淘汰（eviction）与 `Expire` 通过一次全表扫描按闭区间 `ExpiresAt <= Now` 过滤，单表规模为控制面元数据量级时足够简单可靠。

**候选事务**：`Apply` 采用两阶段提交式候选状态。先对整个批次做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再检查时间单调性；随后在克隆的候选 map 上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 各分配一个递增 revision）。最终容量超限或任何错误发生时直接丢弃候选，淘汰、时间与 revision 一并回滚，表状态保持不变。成功时原子换入候选、推进 `now` 并将 generation 加一（空批次不改变任何状态）。

**所有权**：所有公开方法通过单个 `sync.Mutex` 串行化，支持并发调用。`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响内部状态；`Entry` 为纯值类型，无共享指针。

**复杂度**：`Apply` 为 O(E + B)，其中 E 为当前条目数（克隆+淘汰扫描）、B 为批内操作数；`Expire` 与 `Snapshot` 为 O(E log E)（结果按键排序，排序仅为确定性输出）；空间 O(E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
