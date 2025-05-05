# Gorig-OM

[English](#english) | [中文](#chinese)

<a id="english"></a>
# Gorig-OM

Gorig-OM is the operations management panel for the [Gorig](https://github.com/jom-io/gorig) project, providing an intuitive web interface to manage and monitor your Gorig services.

## Features

- Intuitive web management interface
- Service status monitoring
- Configuration management
- Real-time log viewing
- Secure access control

## Quick Start

### 1. Installation

First, install the package:

```bash
go get github.com/jom-io/gorig-om@latest
```

Then, add the following configuration to your Gorig project:

```yaml
om:
  key: "your-access-key-here"  # Set access password
```

### 2. Enable OM

You can enable the operations management panel in one of two ways:

1. Import the package in your main.go:
```go
import _ "github.com/jom-io/gorig-om/src"
```

2. Or call the setup function in your code:
```go
import "github.com/jom-io/gorig-om/src"

func main() {
    om.Setup()
    // ... your other code
}
```

### 3. Access Panel

After configuration, you can access the operations panel at:

[https://jom-io.github.io/gorig-om](https://jom-io.github.io/gorig-om)

Use the `om.key` you set in the configuration for access authentication.

## Security Notes

- Please ensure you set a sufficiently complex access password
- It is recommended to use HTTPS in production environments
- Regularly change the access password to improve security

## Related Projects

- [Gorig](https://github.com/jom-io/gorig) - Main project repository

## Credits

The admin panel UI is built using [Slash Admin](https://github.com/d3george/slash-admin).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details

---

<a id="chinese"></a>
# Gorig-OM

Gorig-OM 是 [Gorig](https://github.com/jom-io/gorig) 项目的运维管理面板，提供了一个直观的 Web 界面来管理和监控您的 Gorig 服务。

## 功能特点

- 直观的 Web 管理界面
- 服务状态监控
- 配置管理
- 实时日志查看
- 安全访问控制

## 快速开始

### 1. 安装

首先，安装包：

```bash
go get github.com/jom-io/gorig-om@latest
```

然后，在您的 Gorig 项目中添加以下配置：

```yaml
om:
  key: "your-access-key-here"  # 设置访问密码
```

### 2. 启用 OM

您可以通过以下两种方式之一启用运维管理面板：

1. 在 main.go 中导入包：
```go
import _ "github.com/jom-io/gorig-om/src"
```

2. 或在代码中调用设置函数：
```go
import "github.com/jom-io/gorig-om/src"

func main() {
    om.Setup()
    // ... 其他代码
}
```

### 3. 访问面板

配置完成后，您可以通过以下地址访问运维面板：

[https://jom-io.github.io/gorig-om](https://jom-io.github.io/gorig-om)

使用您在配置中设置的 `om.key` 进行访问认证。

## 安全说明

- 请确保设置一个足够复杂的访问密码
- 建议在生产环境中使用 HTTPS
- 定期更换访问密码以提高安全性

## 相关项目

- [Gorig](https://github.com/jom-io/gorig) - 主项目仓库

## 致谢

管理面板 UI 基于 [Slash Admin](https://github.com/d3george/slash-admin) 实现。

## 许可证

本项目采用 MIT 许可证 - 详见 [LICENSE](LICENSE) 文件 