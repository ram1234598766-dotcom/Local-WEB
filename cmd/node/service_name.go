package main

// serviceName is the name Windows shows in services.msc and the name SCM keys
// the service under in HKLM\SYSTEM\CurrentControlSet\Services. It is passed to
// svc.Run and reported in the node's own log lines.
//
// It is deliberately not a windows-only constant. The MSI names the service
// independently, in three places, so a typo on either side is a runtime failure
// rather than a build failure: svc.Run returns ERROR_FAILED_SERVICE_CONTROLLER_
// CONNECT, SCM logs event 7009 after 60 seconds, and the package reports
// "Error 1920. Service 'LocalWEB Mesh Network' failed to start." That reads
// like a crash in the daemon, not like a misspelled service name, which is why
// service_name_test.go holds the installers to this value instead.
//
// It is also not the systemd unit name. That unit is localweb.service, all
// lowercase, and the two names are unrelated on Linux.
const serviceName = "LocalWEB"
