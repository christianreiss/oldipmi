#include "runtimedir.h"
#include <QDir>
#include <QFile>
#include <cerrno>
#include <cstring>
#include <sys/stat.h>
#include <unistd.h>

static QString base() {
  const QString xdg = qEnvironmentVariable("XDG_RUNTIME_DIR");
  return xdg.isEmpty() ? QDir::tempPath() + "/oldipmi-" + QString::number(getuid()) : xdg;
}
QString runtimeDirPath() { return QDir::cleanPath(base() + "/oldipmi"); }

// Makes dir if missing and checks that it is this user's directory and private.
static bool privateLevel(const QString &dir, QString *error) {
  const QByteArray path = QFile::encodeName(dir);
  if (mkdir(path.constData(), 0700) != 0 && errno != EEXIST) {
    *error = "Cannot create " + dir + ": " + QString::fromLocal8Bit(strerror(errno));
    return false;
  }
  struct stat st;
  if (lstat(path.constData(), &st) != 0 || !S_ISDIR(st.st_mode) || st.st_uid != getuid()) {
    *error = dir + " is not a directory of this user; OldIPMI does not put its sockets there.";
    return false;
  }
  if ((st.st_mode & 077) && chmod(path.constData(), 0700) != 0) {
    *error = "Cannot make " + dir + " private: " + QString::fromLocal8Bit(strerror(errno));
    return false;
  }
  return true;
}
QString runtimeDir(QString *error) {
  QString why;
  const bool shared = qEnvironmentVariableIsEmpty("XDG_RUNTIME_DIR");
  if ((shared && !privateLevel(base(), &why)) || !privateLevel(runtimeDirPath(), &why)) {
    if (error)
      *error = why;
    return {};
  }
  return runtimeDirPath();
}
