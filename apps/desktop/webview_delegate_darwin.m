#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import "webview_delegate_darwin.h"

@interface MulticaUIDelegate : NSObject <WKUIDelegate>
@end

@implementation MulticaUIDelegate

- (void)webView:(WKWebView *)webView 
runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters 
initiatedByFrame:(WKFrameInfo *)frame 
completionHandler:(void (^)(NSArray<NSURL *> * _Nullable URLs))completionHandler {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSOpenPanel *panel = [NSOpenPanel openPanel];
        [panel setAllowsMultipleSelection:parameters.allowsMultipleSelection];
        [panel setCanChooseDirectories:parameters.allowsDirectories];
        [panel setCanChooseFiles:YES];
        [panel setResolvesAliases:YES];
        
        NSWindow *window = webView.window;
        if (window) {
            [panel beginSheetModalForWindow:window completionHandler:^(NSModalResponse result) {
                if (result == NSModalResponseOK) {
                    completionHandler(panel.URLs);
                } else {
                    completionHandler(nil);
                }
            }];
        } else {
            NSInteger result = [panel runModal];
            if (result == NSModalResponseOK) {
                completionHandler(panel.URLs);
            } else {
                completionHandler(nil);
            }
        }
    });
}

@end

static MulticaUIDelegate *globalUIDelegate = nil;

void SetupWebviewUIDelegate(void* nsWindowHandle) {
    if (!nsWindowHandle) return;
    
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!globalUIDelegate) {
            globalUIDelegate = [[MulticaUIDelegate alloc] init];
        }
        
        NSWindow *window = (__bridge NSWindow*)nsWindowHandle;
        if (!window) return;
        
        NSView *contentView = [window contentView];
        if (!contentView) return;
        
        NSMutableArray *viewsToInspect = [NSMutableArray arrayWithObject:contentView];
        while ([viewsToInspect count] > 0) {
            NSView *currentView = [viewsToInspect firstObject];
            [viewsToInspect removeObjectAtIndex:0];
            
            if ([currentView isKindOfClass:[WKWebView class]]) {
                WKWebView *wv = (WKWebView*)currentView;
                wv.UIDelegate = globalUIDelegate;
                return;
            }
            
            [viewsToInspect addObjectsFromArray:[currentView subviews]];
        }
    });
}
