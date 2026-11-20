#ifndef WEBVIEW_DELEGATE_DARWIN_H
#define WEBVIEW_DELEGATE_DARWIN_H

#ifdef __cplusplus
extern "C" {
#endif

void SetupWebviewUIDelegate(void* nsWindowHandle);
void SetupDarwinURLHandler(void);

#ifdef __cplusplus
}
#endif

#endif /* WEBVIEW_DELEGATE_DARWIN_H */
