# Storage 设计决策

> 上级: [Storage 系统设计](README.md)

---

## ST-001：SQLite 是默认根 KV

使用 `modernc.org/sqlite` 的纯 Go驱动，默认单文件 `./data/yaa.db`。它满足零 CGO、单文件备份和目标平台交叉编译；WAL 与 busy timeout 是实现常量，不增加配置旋钮。

## ST-002：v1 只有 sqlite 和 memory

Memory 后端覆盖测试/临时运行，SQLite 覆盖持久化。第三种后端在有测量证据前不进入 enum，避免额外依赖和迁移矩阵。

## ST-003：TTL 是接口能力，不是 Session 生命周期

`Set(key,value,ttl...)` 保留可选 TTL，所有后端一致实现惰性隐藏和 batch cleanup。Session snapshot 永不传 TTL；它的 TTL/max lifetime 由 Session 状态机处理。

## ST-004：Close 属于核心接口

Runtime 必须可靠释放数据库和 worker，因此 `Close()` 直接属于 Storage。所有实现都支持幂等 Close，不需要调用方做可选接口类型断言。

## ST-005：Keys 只有前缀和稳定排序

`Keys(prefix)` 足以完成 Session Restore；不提供全查询 DSL。SQLite 使用 literal prefix SQL，memory 使用 `strings.HasPrefix`，两者都按 key 字节升序。

## ST-006：Memory 不复用根 KV 接口

Memory item 需要复合主键、Version、查询排序和原子 upsert，使用专用 ContentStore。根 Storage v1 只保存 `session:<id>` 完整 snapshot。

## ST-007：固定安全边界

key 512 bytes、value 16 MiB、cleanup tick 60 秒和 batch 1000 是 v1 常量。实际负载证明需要调整前不增加配置字段；两种后端使用相同限制。

## ST-008：严格恢复

SQLite schema 版本未知、integrity check 失败或任何 Session snapshot 损坏都会使 Runtime Not Ready。Storage 不跳过、删除或自动修复业务 values；恢复决策属于 Session Manager。

---

*最后更新: 2026-07-22*
