import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../providers/auth_provider.dart';
import '../theme/app_theme.dart';
import '../utils/api_error.dart';

class ServerLoginScreen extends ConsumerStatefulWidget {
  const ServerLoginScreen({super.key});

  @override
  ConsumerState<ServerLoginScreen> createState() => _ServerLoginScreenState();
}

class _ServerLoginScreenState extends ConsumerState<ServerLoginScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _serverController;
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _obscurePassword = true;
  bool _isSubmitting = false;

  @override
  void initState() {
    super.initState();
    _serverController = TextEditingController(
      text: ref.read(authProvider).serverUrl ?? '',
    );
  }

  @override
  void dispose() {
    _serverController.dispose();
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_isSubmitting || !(_formKey.currentState?.validate() ?? false)) {
      return;
    }

    final serverUrl = ApiClient.normalizeServerUrl(_serverController.text);
    _serverController.value = _serverController.value.copyWith(
      text: serverUrl,
      selection: TextSelection.collapsed(offset: serverUrl.length),
    );

    setState(() {
      _isSubmitting = true;
    });

    try {
      await ref
          .read(authProvider.notifier)
          .login(serverUrl, _usernameController.text, _passwordController.text);
    } catch (error) {
      if (mounted) {
        _showError(_messageFor(error));
      }
    } finally {
      if (mounted) {
        setState(() {
          _isSubmitting = false;
        });
      }
    }
  }

  String _messageFor(Object error) {
    // 登录场景对部分错误有更具体的文案
    if (error is ApiException) {
      return switch (error.code) {
        1001 => '请输入完整的登录信息',
        2003 => '用户名或密码错误',
        2001 => '登录状态已失效，请重新登录',
        -1 => '无法连接服务器',
        _ => error.message,
      };
    }
    return describeApiError(error);
  }

  void _showError(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  String? _required(String? value, String label) {
    if (value == null || value.trim().isEmpty) {
      return '请输入$label';
    }
    return null;
  }

  String? _validateServerUrl(String? value) {
    if (value == null || value.trim().isEmpty) {
      return '请输入服务器地址';
    }
    try {
      ApiClient.normalizeServerUrl(value);
      return null;
    } on FormatException catch (error) {
      return error.message;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, constraints) {
            return SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: ConstrainedBox(
                constraints: BoxConstraints(
                  minHeight: constraints.maxHeight - 48,
                  maxWidth: 520,
                ),
                child: Center(
                  child: Form(
                    key: _formKey,
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        const Icon(
                          Icons.play_circle_outline,
                          color: AppTheme.accent,
                          size: 56,
                        ),
                        const SizedBox(height: 20),
                        Text(
                          'fan-web',
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.displaySmall
                              ?.copyWith(
                                fontWeight: FontWeight.w700,
                                color: AppTheme.foreground,
                              ),
                        ),
                        const SizedBox(height: 8),
                        Text(
                          '自托管看番',
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.titleMedium
                              ?.copyWith(
                                color: AppTheme.foreground.withValues(
                                  alpha: 0.7,
                                ),
                              ),
                        ),
                        const SizedBox(height: 40),
                        TextFormField(
                          controller: _serverController,
                          keyboardType: TextInputType.url,
                          textInputAction: TextInputAction.next,
                          autocorrect: false,
                          decoration: const InputDecoration(
                            labelText: '服务器地址',
                            hintText: 'http://192.168.1.100:8080',
                            prefixIcon: Icon(Icons.dns_outlined),
                          ),
                          validator: _validateServerUrl,
                        ),
                        const SizedBox(height: 16),
                        TextFormField(
                          controller: _usernameController,
                          textInputAction: TextInputAction.next,
                          autofillHints: const [AutofillHints.username],
                          decoration: const InputDecoration(
                            labelText: '用户名',
                            prefixIcon: Icon(Icons.person_outline),
                          ),
                          validator: (value) => _required(value, '用户名'),
                        ),
                        const SizedBox(height: 16),
                        TextFormField(
                          controller: _passwordController,
                          obscureText: _obscurePassword,
                          textInputAction: TextInputAction.done,
                          autofillHints: const [AutofillHints.password],
                          onFieldSubmitted: (_) => _submit(),
                          decoration: InputDecoration(
                            labelText: '密码',
                            prefixIcon: const Icon(Icons.lock_outline),
                            suffixIcon: IconButton(
                              tooltip: _obscurePassword ? '显示密码' : '隐藏密码',
                              icon: Icon(
                                _obscurePassword
                                    ? Icons.visibility_outlined
                                    : Icons.visibility_off_outlined,
                              ),
                              onPressed: () {
                                setState(() {
                                  _obscurePassword = !_obscurePassword;
                                });
                              },
                            ),
                          ),
                          validator: (value) => _required(value, '密码'),
                        ),
                        const SizedBox(height: 24),
                        FilledButton.icon(
                          onPressed: _isSubmitting ? null : _submit,
                          icon: _isSubmitting
                              ? const SizedBox.square(
                                  dimension: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    color: AppTheme.background,
                                  ),
                                )
                              : const Icon(Icons.login),
                          label: Text(_isSubmitting ? '登录中...' : '登录'),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}
