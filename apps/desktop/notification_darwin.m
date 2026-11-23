#import <Cocoa/Cocoa.h>
#import <UserNotifications/UserNotifications.h>
#import "notification_darwin.h"
#include "_cgo_export.h"

@interface MulticaNotificationDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation MulticaNotificationDelegate

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions options))completionHandler {
    if (@available(macOS 11.0, *)) {
        completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionSound);
    } else {
        completionHandler(UNNotificationPresentationOptionAlert | UNNotificationPresentationOptionSound);
    }
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
didReceiveNotificationResponse:(UNNotificationResponse *)response
         withCompletionHandler:(void (^)(void))completionHandler {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
        NSWindow *win = [NSApp mainWindow] ?: [[NSApp windows] firstObject];
        if (win) {
            if ([win isMiniaturized]) {
                [win deminiaturize:nil];
            }
            [win makeKeyAndOrderFront:nil];
        }
    });

    NSDictionary *userInfo = response.notification.request.content.userInfo;
    NSString *payload = userInfo[@"payload"];
    if (payload && [payload isKindOfClass:[NSString class]]) {
        multicaOnNotificationClicked((char *)[payload UTF8String]);
    } else {
        multicaOnNotificationClicked((char *)"");
    }

    completionHandler();
}

@end

static MulticaNotificationDelegate *globalNotificationDelegate = nil;

static BOOL canUseUserNotifications(void) {
    NSString *bundleID = [[NSBundle mainBundle] bundleIdentifier];
    return bundleID != nil && [bundleID length] > 0;
}

void InitMacNotifications(void) {
    if (!canUseUserNotifications()) {
        return;
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!globalNotificationDelegate) {
            globalNotificationDelegate = [[MulticaNotificationDelegate alloc] init];
        }
        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
        center.delegate = globalNotificationDelegate;
        [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound | UNAuthorizationOptionBadge)
                              completionHandler:^(BOOL granted, NSError * _Nullable error) {
            (void)granted;
            (void)error;
        }];
    });
}

void ShowMacNotification(const char* title, const char* body, const char* payloadJSON) {
    if (!canUseUserNotifications()) {
        return;
    }
    NSString *nsTitle = title ? [NSString stringWithUTF8String:title] : @"Multica";
    NSString *nsBody = body ? [NSString stringWithUTF8String:body] : @"";
    NSString *nsPayload = payloadJSON ? [NSString stringWithUTF8String:payloadJSON] : @"";

    dispatch_async(dispatch_get_main_queue(), ^{
        if (!globalNotificationDelegate) {
            globalNotificationDelegate = [[MulticaNotificationDelegate alloc] init];
            [UNUserNotificationCenter currentNotificationCenter].delegate = globalNotificationDelegate;
        }
        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];

        UNMutableNotificationContent *content = [[UNMutableNotificationContent alloc] init];
        content.title = nsTitle;
        content.body = nsBody;
        content.sound = [UNNotificationSound defaultSound];
        if ([nsPayload length] > 0) {
            content.userInfo = @{ @"payload": nsPayload };
        }

        NSString *identifier = [[NSUUID UUID] UUIDString];
        UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:identifier
                                                                              content:content
                                                                              trigger:nil];
        [center addNotificationRequest:request withCompletionHandler:^(NSError * _Nullable error) {
            (void)error;
        }];
    });
}
