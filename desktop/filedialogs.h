#pragma once
#include <QString>

// The file choosers the window opens: modal, without a parent window, as
// QFileDialog's static functions open them. A test hands Controller its own
// to choose without a dialog.
class FileDialogs {
public:
  virtual ~FileDialogs() = default;
  // An ISO image to mount; empty when the user cancels.
  virtual QString isoImage();
  // A PEM file with a client certificate, or with its private key; empty
  // when the user cancels.
  virtual QString clientCertificate(bool key);
};
