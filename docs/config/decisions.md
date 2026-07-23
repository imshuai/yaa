# Config 设计决策

> 文档路径: `docs/config/decisions.md`
> 上级: `docs/config/README.md` §9

---

## 9. 设计决策

### CF-001: 多源合并优先级固定为四层

**决策：** 配置来源优先级固定为四层：内置默认值 → 配置文件 → 环境变量引用 → 命令行参数。

**理由：**
- 四层模型覆盖绝大多数部署场景（开发、测试、生产）
- 层级固定，用户心智模型清晰
- 环境变量引用是配置文件的值展开层，而非独立来源

**影响：** Loader 按固定顺序依次覆盖，不提供可自定义的优先级或独立合并组件。

### CF-002: 主推 YAML，兼容 TOML 和 JSON

**决策：** 配置文件主推 YAML，同时兼容 TOML 和 JSON，通过文件扩展名自动检测。

**理由：**
- YAML 可读性最佳，适合手写和注释
- TOML 在部分生态中流行，兼容降低迁移成本
- JSON 适合程序生成和 API 交互

**影响：** Loader 需实现三种格式解析器，统一转换为内部结构体。

### CF-003: 环境变量引用使用 `${VAR_NAME}` 语法

**决策：** 配置文件中通过 `${VAR_NAME}` 引用环境变量，在解析后、校验前展开。

**理由：**
- `${}` 是跨语言通用约定（Docker Compose、Spring），用户熟悉
- 展开时机在校验前，确保展开后的值参与校验
- 支持 `${VAR:-default}`；不实现其他 shell 参数展开、嵌套或递归展开

**影响：** EnvVar Resolver 在原始 Map 迁移后、`ApplyElementDefaults(raw)` 前执行。

### CF-004: 敏感信息只走环境变量，不写入配置文件

**决策：** API Key、密码、Token 等敏感信息通过环境变量引用注入，不硬编码到配置文件。

**理由：**
- 配置文件可能被提交到版本控制，存在泄露风险
- 环境变量是容器化部署管理密钥的标准方式
- 与 Kubernetes Secret、Docker Secret 等机制兼容

**影响：** 文档需明确标注敏感字段，推荐使用环境变量引用。

### CF-005: 热更新基于文件监听 + 原子替换

**决策：** 配置热更新通过 fsnotify 文件监听触发，串行完成完整 Load/diff 后通过 `atomic.Value` 原子替换；出现任一需重启字段时整批拒绝。

**理由：**
- 文件监听是配置热更新的标准方案，无需引入额外服务
- 原子替换保证读取一致性，不会读到半解析状态
- 读取无锁；单个 reload mutex 只串行化低频写入

**影响：** 各模块持有 Effective Config 引用，热更新后需重新读取。

### CF-006: 校验使用结构体 Tag + 自定义校验函数

**决策：** 通过结构体 Tag（如 `validate:"required,min=1"`）声明字段约束，复杂规则用自定义函数补充。

**理由：**
- Tag 声明式校验简洁直观，覆盖大部分场景
- 自定义函数处理跨字段依赖和业务规则
- 使用成熟库（go-playground/validator）减少自研成本

**影响：** 配置结构体需定义完善 Tag，复杂逻辑集中在 Validator 模块。

### CF-007: 默认值通过结构体初始化注入，不使用反射

**决策：** 内置默认值通过结构体字面量初始化注入，不通过反射或 Tag 设置默认值。

**理由：**
- 字面量初始化性能最优，无运行时开销
- 代码可读性高，默认值一目了然
- 编译期保证类型安全

**影响：** 新增配置字段时需同步更新默认值初始化代码。

### CF-008: 配置版本化 + 迁移函数链

**决策：** 配置文件包含 `config_version` 字段，版本间通过显式迁移图自动升级，不推测 `nextVersion`。

**理由：**
- 版本化使配置变更可追踪
- 迁移链支持多版本跨度升级（v1→v3 自动经过 v1→v2→v3）
- 迁移逻辑可测试、可回溯，用户升级时配置自动迁移

**影响：** 每次破坏性配置变更需编写迁移函数并注册到迁移链。

### 决策总览

| 编号 | 决策摘要 | 核心权衡 |
|------|----------|----------|
| CF-001 | 四层固定优先级 | 简单性 > 灵活性 |
| CF-002 | YAML 优先，兼容 TOML/JSON | 可读性 > 单一格式 |
| CF-003 | `${VAR_NAME}` / `${VAR_NAME:-default}` | 简单性 > shell 兼容 |
| CF-004 | 敏感信息只走环境变量 | 安全 > 便利 |
| CF-005 | fsnotify + atomic.Value | 无锁 > 一致性锁 |
| CF-006 | Tag + 自定义函数 | 声明式 + 命令式混合 |
| CF-007 | 字面量初始化默认值 | 性能 + 可读性 > 反射灵活性 |
| CF-008 | 版本化 + 迁移链 | 自动迁移 > 手动修改 |

---

## 10. 模块关系

```text
┌────────────────────────────────────────────────┐
│                  Config System                  │
│                                                  │
│  Loader (YAML/TOML/JSON 自动检测)               │
│    │                                             │
│    ▼                                             │
│  Migration Graph (原始 Map 版本迁移)             │
│    │                                             │
│    ▼                                             │
│  EnvVar Resolver (${VAR} 展开)                   │
│    │                                             │
│    ▼                                             │
│  Defaulting (Default + ApplyElementDefaults)      │
│  DecodeInto + CLI Flags                            │
│    │                                             │
│    ▼                                             │
│  Validator (Tag + 自定义函数)                    │
│    │                                             │
│    ▼                                             │
│  Effective Config (atomic.Value, 全局只读快照)   │
│    │                                             │
│    ├──► Watcher (fsnotify, 文件变更重新加载)     │
│    └──► 各运行时模块 (Agent/Provider/Tool...)    │
│                                                  │
│  Watcher 触发时: Loader → ... → 原子替换          │
└────────────────────────────────────────────────┘

依赖方向:
  Loader → Parsers → Migration → EnvVar Resolver → Defaulting
         → DecodeInto → CLI Flags → Validator → Effective Config
  Watcher → Loader (文件变更时重新加载)
  Migration Graph → Loader (版本不匹配时迁移后加载)
  各运行时模块 → Effective Config (只读引用)

关键约束:
  - Effective Config 全局只读，写入仅通过原子替换
  - 热更新复用同一 Loader，因此同样执行版本检查和迁移
  - Validator 仅校验不修改值；`Default()`/`Default*Config` 与 `ApplyElementDefaults(raw)` 共同完成唯一默认值阶段
  - 配置系统不依赖任何运行时模块，是纯粹的基础设施层
```

---

*最后更新: 2025-07-17*
