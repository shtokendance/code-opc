#ifndef NOTIFICATION_DARWIN_H
#define NOTIFICATION_DARWIN_H

void InitMacNotifications(void);
void ShowMacNotification(const char* title, const char* body, const char* payloadJSON);

#endif
