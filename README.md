# featureflags

并发安全的内存型功能开关目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
`Store` 内部以 `map[string]Record` 作为主索引，键为开关名称，值为记录（含深拷贝的
`Value` 与单调递增的 `Revision`）。单把 `sync.Mutex` 保护全部状态（索引、
`generation`、`revision`），所有公开方法（`Apply`/`Get`/`Snapshot`）在同一互斥锁下
执行，因此天然并发安全且批次之间满足线性一致性。

### 候选事务
`Apply` 采用“候选事务”（copy-on-write candidate）：
1. 在锁内先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），
   任何 `ErrInvalidInput` 都在读取状态之前返回；
2. 将当前索引浅拷贝为候选 map（Record 中的 Value 切片在库内不可变，浅拷贝即安全），
   按输入顺序在候选上执行 Put/Delete——Put 分配连续 revision，Delete 不分配，
   Delete 缺失键立即以 `ErrNotFound` 中止；
3. 仅在批次末检查最终记录数与 Value 总字节容量（`ErrCapacity`）；
4. 全部通过才一次性替换内部索引并推进 `revision`；非空成功批次 `generation` 只 +1。

任何失败路径都不触碰内部状态，因此状态、`generation`、`revision` 自动完整回滚，
无需显式 undo 日志。

### 所有权
写入时拷贝调用方传入的 `Value`；`Get`/`Snapshot`/`Result.Changed` 返回的 `Value`
均为深拷贝。返回的切片与内部状态完全隔离，调用方可自由修改或持有，库也永不回写
调用方的切片。

### 复杂度
设 n 为当前记录数，k 为批次内操作数：
- `Apply`：时间 O(n + k)（候选拷贝 + 顺序执行 + 末次容量统计），空间 O(n)；
- `Get`：O(1) 均摊（外加一次 Value 拷贝）；
- `Snapshot`：O(n log n)（按名称排序）+ O(总字节数) 深拷贝；
- 所有操作互斥串行化，无锁内分配之外的系统调用。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
