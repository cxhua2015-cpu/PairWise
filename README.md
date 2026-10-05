# resourcelease174

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：主索引为 `map[string]Entry`，键到租约条目 O(1) 定位。`Snapshot` 与 `Expire` 返回的条目按字典序排序，保证输出确定性。未维护按过期时间的辅助索引——淘汰与过期均为一次全表扫描，条目数受 `MaxEntries` 上限约束，成本可控。

**候选事务**：`Apply` 先对整个批次做完整结构校验（时间非负、kind 合法、键字符集与长度），再检查单调时间，然后在当前状态的候选副本上执行：先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序应用 Put/Touch/Delete，Put/Touch 各分配一个 revision。最终容量检查失败或任一操作出错时，候选副本连同淘汰、时间与 revision 一并丢弃，表状态完全不变；只有全部成功才一次性提交。非空成功批次 generation 恰好加一，空批次不变。

**所有权**：表内部条目绝不外借。`Expire` 与 `Snapshot` 返回的切片均为新建副本，调用方修改返回值不影响表内状态；`Entry` 为纯值类型，无共享指针。

**并发**：所有公开方法通过单一 `sync.Mutex` 串行化，可安全并发调用；`Apply` 的纯校验阶段（不读共享状态）在持锁前完成。

**复杂度**（n = 当前条目数，m = 批次操作数）：
- `Apply`：校验 O(m·k)（k 为键长），候选复制 O(n)，执行 O(m)，整体 O(n + m·k)。
- `Expire`：O(n) 扫描 + O(e log e) 排序（e 为过期条目数）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
