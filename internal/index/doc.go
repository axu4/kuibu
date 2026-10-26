// Package index 负责 SQLite 索引，可随时删除重建；外部改文件时经 fsnotify 检测并增量重建。
package index
