# resourcelease129

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约名，Put/Touch/Delete 与过期扫描均为哈希访问。
- 未维护额外的按过期时间排序的堆/树索引：过期采用全表扫描（`ExpiresAt <= Now` 闭区间），
  在表容量受 `MaxEntries` 上限约束的前提下足够简单且成本可控。

### 候选事务
- `Apply` 分两阶段：先对整个批次做完整结构校验（kind、键字符集与字节上限、非负时间），
  通过后才加锁检查时间单调性。
- 随后在**候选状态**（当前条目的副本）上执行：先淘汰 `ExpiresAt <= Now` 的条目，
  再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision。
- 最终容量检查失败或任何中途错误（如 Touch/Delete 缺失键）都会整体回滚——
  淘汰、时间和 revision 分配全部不落盘，只有全部成功才一次性提交。
- 非空成功批次 generation 恰好 +1；空批次不改变 generation（但仍推进单调时间）。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 保护，可任意并发调用。
- 返回值（`Snapshot.Entries`、`Expire` 结果切片）均为新分配的副本，
  调用方修改不影响表内状态；表也不保留调用方传入的切片。

### 复杂度
- `Apply`：结构校验 O(批次大小)，候选复制与过期扫描 O(n)，操作执行 O(批次大小)，n 为当前条目数。
- `Expire`：O(n) 扫描 + O(k log k) 结果排序（按键排序保证确定性），k 为过期条目数。
- `Snapshot`：O(n log n)（复制并按键排序）。
- 空间：O(n)，n ≤ `MaxEntries`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
