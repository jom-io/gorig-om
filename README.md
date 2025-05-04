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

Add the following configuration to your Gorig project:

```yaml
om:
  key: "your-access-key-here"  # Set access password
```

### 2. Access Panel

After configuration, you can access the operations panel at:

[https://jom-io.github.io/gorig-om](https://jom-io.github.io/gorig-om)

Use the `om.key` you set in the configuration for access authentication.

## Security Notes

- Please ensure you set a sufficiently complex access password
- It is recommended to use HTTPS in production environments
- Regularly change the access password to improve security

## Related Projects

- [Gorig](https://github.com/jom-io/gorig) - Main project repository

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

在您的 Gorig 项目中添加以下配置：

```yaml
om:
  key: "your-access-key-here"  # 设置访问密码
```

### 2. 访问面板

配置完成后，您可以通过以下地址访问运维面板：

[https://jom-io.github.io/gorig-om](https://jom-io.github.io/gorig-om)

使用您在配置中设置的 `om.key` 进行访问认证。

## 安全说明

- 请确保设置一个足够复杂的访问密码
- 建议在生产环境中使用 HTTPS
- 定期更换访问密码以提高安全性

## 相关项目

- [Gorig](https://github.com/jom-io/gorig) - 主项目仓库

## 许可证

本项目采用 MIT 许可证 - 详见 [LICENSE](LICENSE) 文件 