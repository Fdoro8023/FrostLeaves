package main

import (
	"log"

	"frostleaves/pkg/migration"
)

// migrateOnStartup backs up and migrates the data directory when its schema is
// older than this build, then validates the result. It is a no-op when the data
// is already current (模块 1：版本升级数据迁移).
func migrateOnStartup(dataDir, appVersion string) error {
	from, err := migration.DetectVersion(dataDir)
	if err != nil {
		return err
	}
	if from == migration.SchemaVersion {
		return nil
	}
	res, err := migration.Run(migration.Options{
		DataDir:    dataDir,
		AppVersion: appVersion,
		Logf:       log.Printf,
	})
	if err != nil {
		return err
	}
	if res.Already {
		return nil
	}
	log.Printf("[Migration] schema upgraded v%d -> v%d; steps=%v; backup=%s",
		res.FromVersion, res.ToVersion, res.Applied, res.BackupDir)
	return nil
}

// dataSchemaVersion reports the schema version of the data directory for
// display in the server UI (模块 1). Unreadable/missing stamps read as legacy 0.
func dataSchemaVersion(dataDir string) int {
	v, err := migration.DetectVersion(dataDir)
	if err != nil {
		return -1
	}
	return v
}

// rollbackFromBackup restores the data directory from a pre-upgrade backup and
// reports the restored schema version (一键回滚).
func rollbackFromBackup(dataDir, backupDir string) error {
	if err := migration.Rollback(dataDir, backupDir); err != nil {
		return err
	}
	v, err := migration.DetectVersion(dataDir)
	if err != nil {
		return err
	}
	log.Printf("[Migration] rollback complete; data directory restored to schema v%d", v)
	return nil
}
