class User {
  const User({
    required this.id,
    required this.username,
    required this.isAdmin,
    required this.createdAt,
  });

  final int id;
  final String username;
  final bool isAdmin;
  final String createdAt;

  Map<String, dynamic> toJson() => {
        'id': id,
        'username': username,
        'is_admin': isAdmin,
        'created_at': createdAt,
      };

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: _readInt(json['id']),
      username: _readString(json['username']),
      isAdmin: json['is_admin'] == true,
      createdAt: _readString(json['created_at']),
    );
  }

  static int _readInt(Object? value) {
    if (value is num) {
      return value.toInt();
    }
    return int.tryParse(value?.toString() ?? '') ?? 0;
  }

  static String _readString(Object? value) => value?.toString() ?? '';
}

class LoginResponse {
  const LoginResponse({
    required this.token,
    required this.expiresAt,
    required this.user,
  });

  final String token;
  final String expiresAt;
  final User user;

  factory LoginResponse.fromJson(Map<String, dynamic> json) {
    final userJson = json['user'];
    if (userJson is! Map) {
      throw const FormatException('登录响应缺少用户信息');
    }

    return LoginResponse(
      token: json['token']?.toString() ?? '',
      expiresAt: json['expires_at']?.toString() ?? '',
      user: User.fromJson(Map<String, dynamic>.from(userJson)),
    );
  }
}
