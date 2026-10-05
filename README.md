# resourcecatalog141

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 分层架构

- **状态引擎（`resourcecatalog141/servicecatalog.go`）**：`Store` 持有事务性数据，提供 `Apply`/`Get`/`Snapshot`。
- **策略层（`resourcecatalog141/policy.go`）**：`Policy` 维护可原子替换的 actor 白名单与单批操作数上限，`Authorize` 独立同步。
- **协调层（`resourcecatalog141/coordinator.go`）**：`Coordinator` 先授权再调用引擎，并为成功、拒绝与引擎失败分配连续审计序号。

## 索引

`Store` 以 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`Snapshot` 与批次 `Changed` 在读取时按名称排序输出，不维护额外的有序结构。

## 候选事务

`Apply` 先在完整结构校验（kind、名称字符集与长度、Value 长度）通过后，于当前状态的候选副本（map 浅拷贝 + Value 深拷贝）上按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查。任一步失败即丢弃候选，状态、generation 与 revision 全部回滚；成功时整体换入候选，generation 只增加一次（空批次不变）。

## 所有权

所有跨越 API 边界的 `Value` 字节切片均深拷贝：Put 写入时拷贝入站数据，`Get`/`Snapshot`/`Result.Changed` 返回独立副本；`Decisions` 返回内部审计日志的拷贝。调用方对返回值的任何修改都不会影响内部状态，反之亦然。

## 并发

三层各自使用 `sync.Mutex`/`sync.RWMutex` 保护内部状态，所有公开方法可并发调用。审计序号在协调层锁内单调递增，保证无空洞。

## 复杂度

- `Apply`：O(k + n)，k 为批内操作数，n 为当前记录数（候选拷贝与容量合计）；另加 O(k log k) 的 `Changed` 排序。
- `Get`：O(1) 均摊（外加返回值拷贝 O(v)，v 为 Value 长度）。
- `Snapshot`：O(n log n)（排序）+ O(Σv)（深拷贝）。
- `Authorize`：O(1)；`ReplaceActors`：O(a)，a 为 actor 数；`Decisions`：O(d)，d 为审计条数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
