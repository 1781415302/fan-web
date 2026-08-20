import 'package:fan_web/api/anime_api.dart';
import 'package:fan_web/api/api_client.dart';
import 'package:fan_web/api/bangumi_api.dart';
import 'package:fan_web/api/library_api.dart';
import 'package:fan_web/api/progress_api.dart';
import 'package:fan_web/models/anime.dart';
import 'package:fan_web/models/user.dart';
import 'package:fan_web/providers/auth_provider.dart';

const testAdmin = User(
  id: 1,
  username: 'admin',
  isAdmin: true,
  createdAt: '2026-08-01T00:00:00Z',
);

const testUser = User(
  id: 2,
  username: 'viewer',
  isAdmin: false,
  createdAt: '2026-08-01T00:00:00Z',
);

AuthState adminAuthState() {
  return const AuthState.authenticated(
    user: testAdmin,
    token: 'tok',
    serverUrl: 'http://127.0.0.1:8080',
  );
}

AuthState userAuthState() {
  return const AuthState.authenticated(
    user: testUser,
    token: 'tok',
    serverUrl: 'http://127.0.0.1:8080',
  );
}

class FixedAuthNotifier extends AuthNotifier {
  FixedAuthNotifier(this.fixed);

  final AuthState fixed;

  @override
  AuthState build() {
    super.build();
    return fixed;
  }
}

class FakeAnimeApi extends AnimeApi {
  FakeAnimeApi() : super(ApiClient());

  final List<String> calls = <String>[];
  String? lastKeyword;
  int? lastCreateBangumiId;
  String? lastCreateFilePath;
  Object? createError;
  Object? listError;
  Anime created = const Anime(
    id: 42,
    title: 'Show',
    titleCn: '番剧',
    bangumiId: 101,
    cover: '',
    summary: '',
    epCount: 12,
    filePath: 'ShowDir',
    createdAt: '',
  );
  Anime detail = const Anime(
    id: 7,
    title: 'Show',
    titleCn: '番剧',
    bangumiId: 101,
    cover: '',
    summary: '简介',
    epCount: 12,
    filePath: 'ShowDir',
    createdAt: '',
  );
  List<Episode> episodes = const <Episode>[
    Episode(
      id: 11,
      animeId: 7,
      epNumber: 1,
      title: '01',
      filePath: '01.mkv',
      duration: 0,
    ),
  ];
  PaginatedAnimes listResult = const PaginatedAnimes(
    items: [],
    total: 0,
    page: 1,
    pageSize: 20,
  );

  @override
  Future<PaginatedAnimes> list({
    int page = 1,
    int pageSize = 20,
    String? keyword,
  }) async {
    lastKeyword = keyword;
    calls.add('list:${keyword ?? ''}');
    if (listError != null) {
      throw listError!;
    }
    return listResult;
  }

  @override
  Future<Anime> getById(int id) async {
    calls.add('getById:$id');
    return detail;
  }

  @override
  Future<List<Episode>> listEpisodes(int animeId) async {
    calls.add('listEpisodes:$animeId');
    return episodes;
  }

  @override
  Future<Anime> create(int bangumiId, String filePath) async {
    lastCreateBangumiId = bangumiId;
    lastCreateFilePath = filePath;
    calls.add('create:$bangumiId:$filePath');
    if (createError != null) {
      throw createError!;
    }
    return created.copyWithFilePath(filePath);
  }

  @override
  Future<void> delete(int id) async {
    calls.add('delete:$id');
  }

  @override
  Future<AnimeScanResult> scanAnime(int id) async {
    calls.add('scanAnime:$id');
    return AnimeScanResult(scanned: episodes.length, episodes: episodes);
  }
}

class FakeLibraryApi extends LibraryApi {
  FakeLibraryApi() : super(ApiClient());

  List<UnidentifiedFile> unidentified = const [];
  List<String> dirs = const ['ShowDir', 'Other'];
  List<ScanJob> jobSequence = <ScanJob>[const ScanJob(state: 'done')];
  int _jobIndex = 0;

  @override
  Future<ScanJob> startScan() async {
    return jobSequence.first;
  }

  @override
  Future<ScanJob> getScan() async {
    if (_jobIndex + 1 < jobSequence.length) {
      _jobIndex += 1;
    }
    return jobSequence[_jobIndex];
  }

  @override
  Future<PaginatedUnidentified> listUnidentified({
    int page = 1,
    int pageSize = 50,
  }) async {
    return PaginatedUnidentified(
      items: unidentified,
      total: unidentified.length,
      page: page,
      pageSize: pageSize,
    );
  }

  @override
  Future<List<String>> listDirs() async => dirs;
}

class FakeBangumiApi extends BangumiApi {
  FakeBangumiApi() : super(ApiClient());

  List<BangumiSearchItem> results = const [
    BangumiSearchItem(
      id: 101,
      name: 'Show',
      nameCn: '候选番剧',
      summary: '简介',
      epsCount: 12,
      cover: '',
    ),
  ];

  @override
  Future<List<BangumiSearchItem>> search(String keyword) async => results;

  @override
  Future<BangumiSubject> getSubject(int id) async {
    return BangumiSubject(
      id: id,
      name: 'Show',
      nameCn: '候选番剧',
      summary: '简介',
      cover: '',
      totalEpisodes: 12,
    );
  }
}

class FakeProgressApi extends ProgressApi {
  FakeProgressApi() : super(ApiClient());

  @override
  Future<AnimeProgress> getAnimeProgress(int animeId) async => const [];
}

extension on Anime {
  Anime copyWithFilePath(String path) {
    return Anime(
      id: id,
      title: title,
      titleCn: titleCn,
      bangumiId: bangumiId,
      cover: cover,
      summary: summary,
      epCount: epCount,
      filePath: path,
      createdAt: createdAt,
    );
  }
}

UnidentifiedFile sampleUnidentified({
  String fileName = 'ep01.mkv',
  String filePath = 'ShowDir',
  List<MatchCandidate>? candidates,
}) {
  return UnidentifiedFile(
    fileName: fileName,
    reason: 'ambiguous',
    filePath: filePath,
    candidates:
        candidates ??
        const [
          MatchCandidate(id: 101, name: 'Show', nameCn: '候选番剧', score: 0.91),
        ],
  );
}
