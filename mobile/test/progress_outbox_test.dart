import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:fan_web/providers/auth_provider.dart';
import 'package:fan_web/services/progress_outbox.dart';

void main() {
  late ProviderContainer container;
  late ProgressOutbox outbox;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    final prefs = await SharedPreferences.getInstance();
    container = ProviderContainer(overrides: [
      sharedPreferencesProvider.overrideWithValue(prefs),
    ]);
    outbox = container.read(progressOutboxProvider);
  });

  tearDown(() {
    container.dispose();
  });

  group('ProgressOutbox.save', () {
    test('same episode keeps only latest position', () async {
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      ));
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01T00:01:00Z',
      ));
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.length, 1);
      expect(pending.single.position, 200);
    });

    test('watched=true is not overridden by false', () async {
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 500,
        watched: true,
        updatedAt: '2026-01-01T00:00:00Z',
      ));
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:01:00Z',
      ));
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.single.watched, isTrue);
      expect(pending.single.position, 100);
    });
  });

  group('ProgressOutbox.removeIfMatched', () {
    test('removes exact match', () async {
      const record = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      );
      await outbox.save(record);
      await outbox.removeIfMatched(record);
      final pending = await outbox.getPending('http://a', 1);
      expect(pending, isEmpty);
    });

    test('does not delete if newer record arrived during send', () async {
      const old = PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01T00:00:00Z',
      );
      await outbox.save(old);
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01T00:01:00Z',
      ));
      await outbox.removeIfMatched(old);
      final pending = await outbox.getPending('http://a', 1);
      expect(pending.length, 1);
      expect(pending.single.position, 200);
    });
  });

  group('ProgressOutbox isolation', () {
    test('different users are isolated', () async {
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 2,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      final user1 = await outbox.getPending('http://a', 1);
      final user2 = await outbox.getPending('http://a', 2);
      expect(user1.single.position, 100);
      expect(user2.single.position, 200);
    });

    test('different servers are isolated', () async {
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      await outbox.save(const PendingProgress(
        serverUrl: 'http://b',
        userId: 1,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      final serverA = await outbox.getPending('http://a', 1);
      final serverB = await outbox.getPending('http://b', 1);
      expect(serverA.single.position, 100);
      expect(serverB.single.position, 200);
    });
  });

  group('ProgressOutbox.clearForUser', () {
    test('removes only that user records', () async {
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 1,
        episodeId: 10,
        position: 100,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      await outbox.save(const PendingProgress(
        serverUrl: 'http://a',
        userId: 2,
        episodeId: 10,
        position: 200,
        watched: false,
        updatedAt: '2026-01-01',
      ));
      await outbox.clearForUser(1, 'http://a');
      final user1 = await outbox.getPending('http://a', 1);
      final user2 = await outbox.getPending('http://a', 2);
      expect(user1, isEmpty);
      expect(user2.length, 1);
    });
  });
}
