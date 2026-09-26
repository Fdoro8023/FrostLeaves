import '../../core/i18n/i18n.dart';
class DeviceModel {
  final String id;
  final String name;
  final String platform;
  final String model;
  final String status;
  final String? ipAddress;
  final String createdAt;
  final String updatedAt;

  DeviceModel({
    required this.id,
    required this.name,
    required this.platform,
    this.model = '',
    required this.status,
    this.ipAddress,
    required this.createdAt,
    required this.updatedAt,
  });

  factory DeviceModel.fromJson(Map<String, dynamic> json) {
    return DeviceModel(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      platform: json['platform'] as String? ?? '',
      model: json['model'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      ipAddress: json['ip_address'] as String?,
      createdAt: json['created_at'] as String? ?? '',
      updatedAt: json['updated_at'] as String? ?? '',
    );
  }
}

class FileItem {
  final String name;
  final int size;
  final String type; // "file" or "directory"
  final String modified;

  FileItem({
    required this.name,
    required this.size,
    required this.type,
    required this.modified,
  });

  factory FileItem.fromJson(Map<String, dynamic> json) {
    return FileItem(
      name: json['name'] as String? ?? '',
      size: json['size'] as int? ?? 0,
      type: json['type'] as String? ?? 'file',
      modified: json['modified'] as String? ?? '',
    );
  }

  bool get isDirectory => type == 'directory';

  String get formattedSize {
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KB';
    if (size < 1024 * 1024 * 1024) return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${(size / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }
}

class AuditLogEntry {
  final int id;
  final String deviceId;
  final String action;
  final String? resourcePath;
  final String result;
  final String? detail;
  final String? ipAddress;
  final String createdAt;

  AuditLogEntry({
    required this.id,
    required this.deviceId,
    required this.action,
    this.resourcePath,
    required this.result,
    this.detail,
    this.ipAddress,
    required this.createdAt,
  });

  factory AuditLogEntry.fromJson(Map<String, dynamic> json) {
    return AuditLogEntry(
      id: json['id'] as int? ?? 0,
      deviceId: json['device_id'] as String? ?? '',
      action: json['action'] as String? ?? '',
      resourcePath: json['resource_path'] as String?,
      result: json['result'] as String? ?? '',
      detail: json['detail'] as String?,
      ipAddress: json['ip_address'] as String?,
      createdAt: json['created_at'] as String? ?? '',
    );
  }
}

class QuotaConfig {
  final String? deviceId;
  final int maxUploadBytes;
  final int uploadSpeedKbps;
  final int downloadSpeedKbps;
  final int maxFileSizeBytes;

  QuotaConfig({
    this.deviceId,
    this.maxUploadBytes = 0,
    this.uploadSpeedKbps = 0,
    this.downloadSpeedKbps = 0,
    this.maxFileSizeBytes = 0,
  });

  Map<String, dynamic> toJson() => {
    'device_id': deviceId,
    'max_upload_bytes': maxUploadBytes,
    'upload_speed_kbps': uploadSpeedKbps,
    'download_speed_kbps': downloadSpeedKbps,
    'max_file_size_bytes': maxFileSizeBytes,
  };
}
