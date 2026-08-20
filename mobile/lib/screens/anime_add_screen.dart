import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/bangumi_api.dart';
import '../api/library_api.dart';
import '../providers/anime_provider.dart';
import '../theme/app_theme.dart';
import '../widgets/state_widgets.dart';

class AnimeAddScreen extends ConsumerStatefulWidget {
  const AnimeAddScreen({super.key});

  @override
  ConsumerState<AnimeAddScreen> createState() => _AnimeAddScreenState();
}

class _AnimeAddScreenState extends ConsumerState<AnimeAddScreen> {
  final TextEditingController _keywordController = TextEditingController();
  bool _searching = false;
  String? _searchError;
  List<BangumiSearchItem> _results = const [];
  BangumiSearchItem? _selected;
  String _filePath = '';
  List<String> _dirs = const [];
  String? _dirsError;
  bool _creating = false;
  String? _createError;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        unawaited(_loadDirs());
      }
    });
  }

  @override
  void dispose() {
    _keywordController.dispose();
    super.dispose();
  }

  Future<void> _loadDirs() async {
    try {
      final dirs = await ref.read(libraryApiProvider).listDirs();
      if (!mounted) {
        return;
      }
      setState(() {
        _dirs = dirs;
        _dirsError = null;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _dirsError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _search() async {
    final keyword = _keywordController.text.trim();
    if (keyword.isEmpty) {
      return;
    }
    setState(() {
      _searching = true;
      _searchError = null;
      _results = const [];
      _selected = null;
    });
    try {
      final results = await ref.read(bangumiApiProvider).search(keyword);
      if (!mounted) {
        return;
      }
      setState(() {
        _results = results;
        _searching = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _searching = false;
        _searchError = apiErrorMessage(error);
      });
    }
  }

  Future<void> _create() async {
    final selected = _selected;
    if (selected == null || _creating) {
      return;
    }
    setState(() {
      _creating = true;
      _createError = null;
    });
    try {
      final anime = await ref
          .read(animeApiProvider)
          .create(selected.id, _filePath);
      if (!mounted) {
        return;
      }
      context.pushReplacementNamed(
        'animeDetail',
        pathParameters: {'id': '${anime.id}'},
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _creating = false;
        _createError = apiErrorMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final selected = _selected;
    return Scaffold(
      appBar: AppBar(title: const Text('添加番剧')),
      body: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              key: const Key('bangumi-search'),
              controller: _keywordController,
              textInputAction: TextInputAction.search,
              onSubmitted: (_) => unawaited(_search()),
              decoration: const InputDecoration(
                labelText: '搜索 Bangumi',
                hintText: '输入番剧名称或关键词',
              ),
            ),
            const SizedBox(height: 12),
            FilledButton(
              key: const Key('bangumi-search-submit'),
              onPressed: _searching ? null : () => unawaited(_search()),
              child: Text(_searching ? '搜索中...' : '搜索'),
            ),
            if (_searchError != null) ...[
              const SizedBox(height: 12),
              Text(
                _searchError!,
                style: const TextStyle(color: AppTheme.destructive),
              ),
            ],
            if (!_searching &&
                _keywordController.text.trim().isNotEmpty &&
                _results.isEmpty &&
                _searchError == null) ...[
              const SizedBox(height: 24),
              const EmptyStateView(message: '没有找到相关番剧'),
            ],
            if (_results.isNotEmpty) ...[
              const SizedBox(height: 24),
              Text(
                '选择番剧 · ${_results.length} 条结果',
                style: const TextStyle(
                  color: AppTheme.foreground,
                  fontWeight: FontWeight.w700,
                  fontSize: 16,
                ),
              ),
              const SizedBox(height: 10),
              for (final item in _results)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Material(
                    color: selected?.id == item.id
                        ? AppTheme.accent.withValues(alpha: 0.12)
                        : AppTheme.muted,
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(12),
                      side: BorderSide(
                        color: selected?.id == item.id
                            ? AppTheme.accent
                            : AppTheme.border,
                      ),
                    ),
                    child: ListTile(
                      key: Key('bangumi-item-${item.id}'),
                      title: Text(item.displayName),
                      subtitle: Text(
                        [
                          if (item.nameCn.isNotEmpty && item.name.isNotEmpty)
                            item.name,
                          item.epsCount > 0 ? '全${item.epsCount}话' : '集数未知',
                        ].join(' · '),
                      ),
                      onTap: () {
                        setState(() {
                          _selected = item;
                          _createError = null;
                        });
                      },
                    ),
                  ),
                ),
            ],
            if (selected != null) ...[
              const SizedBox(height: 20),
              const Text(
                '确认添加',
                style: TextStyle(
                  color: AppTheme.foreground,
                  fontWeight: FontWeight.w700,
                  fontSize: 16,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                '${selected.displayName} · Bangumi ID：${selected.id}',
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.72),
                ),
              ),
              const SizedBox(height: 16),
              Text(
                '文件目录',
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.78),
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                '可选。留空表示视频根目录。',
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.55),
                  fontSize: 12,
                ),
              ),
              RadioGroup<String>(
                groupValue: _filePath,
                onChanged: (value) {
                  setState(() {
                    _filePath = value ?? '';
                  });
                },
                child: Column(
                  children: [
                    const RadioListTile<String>(
                      key: Key('dir-root'),
                      title: Text('视频根目录'),
                      value: '',
                    ),
                    for (final dir in _dirs)
                      RadioListTile<String>(
                        key: Key('dir-$dir'),
                        title: Text(dir),
                        value: dir,
                      ),
                  ],
                ),
              ),
              if (_dirsError != null)
                Text(
                  _dirsError!,
                  style: const TextStyle(color: AppTheme.warning, fontSize: 12),
                ),
              if (_createError != null) ...[
                const SizedBox(height: 8),
                Text(
                  _createError!,
                  style: const TextStyle(color: AppTheme.destructive),
                ),
              ],
              const SizedBox(height: 16),
              FilledButton(
                key: const Key('anime-add-submit'),
                onPressed: _creating ? null : () => unawaited(_create()),
                child: Text(_creating ? '添加中...' : '确认添加'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
