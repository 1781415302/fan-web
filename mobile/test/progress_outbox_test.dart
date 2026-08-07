import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/services/progress_outbox.dart';

void main() {
  late SharedPreferences prefs;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    prefs = await SharedPreferences.getInstance();
  });

  // 直接测试 PendingProgress 的序列化和身份键逻辑
  group('PendingProgress', () {
    test('identityKey includes server, user, and episode', () {
      const r = PendingProgress(
        serverUrl: 'http://192.168.1.1:8080',
        userId: 3,
        episodeId: 42,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01',
      );
      expect(r.identityKey, 'http://192.168.1.1:8080|3|42');
    });

    test('toJson/fromJson round-trip', () {
      const original = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: true,
        updatedAt: '2026-08-07T12:00:00Z',
      );
      final json = original.toJson();
      final restored = PendingProgress.fromJson(json);
      expect(restored.serverUrl, 'http://a');
      expect(restored.userId, 1);
      expect(restored.episodeId, 10);
      expect(restored.position, 200);
      expect(restored.watched, isTrue);
      expect(restored.updatedAt, '2026-08-07T12:00:00Z');
    });

    test('watched=true is not overridden by false in merge', () {
      const existing = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 500,
        watched: true,
        updatedAt: '2026-01-01T00:00:00Z',
      );
      const newRecord = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:01:00Z',
      );
      // Simulate merge logic: watched = OR, position = latest
      final merged = PendingProgress(
        serverUrl: newRecord.serverUrl,
        userId: newRecord.userId,
        episodeId: newRecord.episodeId,
        position: newRecord.position,
        watched: newRecord.watched || existing.watched,
        updatedAt: newRecord.updatedAt,
      );
      expect(merged.watched, isTrue);
      expect(merged.position, 100);
    });
  });

  // 测试 storage 层的读写逻辑（模拟 ProgressOutbox 的 _loadAll/_saveAll）
  group('outbox storage', () {
    const storageKey = 'progress_outbox_v1';

    test('same episode keeps only latest position', () async {
      final record1 = const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      ).toJson();
      final record2 = const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01T00:01:00Z',
      ).toJson();

      final map = <String, Map<String, dynamic>>{};
      // Save record1
      final key1 = PendingProgress.fromJson(record1).identityKey;
      map[key1] = record1;
      // Save record2 (same key, should replace)
      final key2 = PendingProgress.fromJson(record2).identityKey;
      map[key2] = record2;

      expect(key1, key2);
      expect(map.length, 1);
      expect(map[key1]!['position'], 200);
    });

    test('different users have different identity keys', () {
      const r1 = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '',
      );
      const r2 = PendingProgress(
        serverUrl: 'http://a',
        userId: 2,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '',
      );
      expect(r1.identityKey, isNot(r2.identityKey));
    });

    test('different servers have different identity keys', () {
      const r1 = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '',
      );
      const r2 = PendingProgress(
        serverUrl: 'http://b',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '',
      );
      expect(r1.identityKey, isNot(r2.identityKey));
    });

    test('storage round-trip preserves data', () async {
      final records = {
        'http://a|1|10': const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 300,
          watched: true,
          updatedAt: '2026-08-07',
        ).toJson(),
      };
      await prefs.setString(storageKey, jsonEncode(records));
      final loaded = jsonDecode(prefs.getString(storageKey)!) as Map<String, dynamic>;
      final restored = PendingProgress.fromJson(
        loaded['http://a|1|10'] as Map<String, dynamic>,
      );
      expect(restored.position, 300);
      expect(restored.watched, isTrue);
    });

    test('clearForUser removes only matching records', () {
      final map = <String, Map<String, dynamic>>{
        'http://a|1|10': const PendingProgress(
          serverUrl: 'http://a',
          userId: 1,
          episodeId: 10,
          position: 100,
          watched: false,
          updatedAt: '',
        ).toJson(),
        'http://a|2|10': const PendingProgress(
          serverUrl: 'http://a',
          userId: 2,
          episodeId: 10,
          position: 200,
          watched: false,
          updatedAt: '',
        ).toJson(),
      };
      map.removeWhere((key, value) {
        final r = PendingProgress.fromJson(value);
        return r.userId == 1 && r.serverUrl == 'http://a';
      });
      expect(map.length, 1);
      expect(map.containsKey('http://a|2|10'), isTrue);
    });
  });
}
