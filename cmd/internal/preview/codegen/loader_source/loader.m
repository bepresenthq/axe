#import <Foundation/Foundation.h>
#import <UIKit/UIKit.h>
#import <dlfcn.h>
#import <pthread.h>
#import <sys/socket.h>
#import <sys/un.h>
#import <unistd.h>

#define LOG(fmt, ...) NSLog(@"[axe-loader] " fmt, ##__VA_ARGS__)

static BOOL runbpInitialReady = NO;
static void (*runbpRefresh)(void) = NULL;

static void *listener_thread(void *arg) {
    const char *sock_path = (const char *)arg;

    // Remove stale socket file if it exists
    unlink(sock_path);

    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) { LOG("socket() failed: %s", strerror(errno)); return NULL; }

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    strlcpy(addr.sun_path, sock_path, sizeof(addr.sun_path));

    if (bind(fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        LOG("bind() failed: %s", strerror(errno));
        close(fd);
        return NULL;
    }
    if (listen(fd, 5) < 0) {
        LOG("listen() failed: %s", strerror(errno));
        close(fd);
        return NULL;
    }

    LOG("Listening on %s", sock_path);

    while (1) {
        int client = accept(fd, NULL, NULL);
        if (client < 0) { LOG("accept() failed: %s", strerror(errno)); continue; }

        char buf[4096];
        ssize_t n = read(client, buf, sizeof(buf) - 1);
        if (n <= 0) { close(client); continue; }
        buf[n] = '\0';

        // Trim trailing newline
        while (n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r')) { buf[--n] = '\0'; }

        if (strcmp(buf, "RUNBP_BARRIER") == 0) {
            __block BOOL ready = NO;
            dispatch_sync(dispatch_get_main_queue(), ^{
                if (runbpInitialReady && runbpRefresh) {
                    // App activation can precede creation of the SwiftUI window.
                    // Retry mounting until an actual preview root exists.
                    BOOL mounted = NO;
                    for (UIWindow *window in [UIApplication sharedApplication].windows) {
                        if ([window.rootViewController.view.accessibilityIdentifier isEqualToString:@"runbp.preview.root"]) mounted = YES;
                    }
                    if (!mounted) runbpRefresh();
                    for (UIWindow *window in [UIApplication sharedApplication].windows) {
                        [window layoutIfNeeded];
                        if ([window.rootViewController.view.accessibilityIdentifier isEqualToString:@"runbp.preview.root"]) ready = YES;
                    }
                }
            });
            // Allow the main-thread transaction to reach the compositor.
            usleep(50000);
            write(client, ready ? "OK\n" : "WAIT\n", ready ? 3 : 5);
            close(client);
            continue;
        }

        LOG("Loading dylib: %s", buf);
        void *handle = dlopen(buf, RTLD_NOW);
        if (handle) {
            LOG("dlopen succeeded");
            typedef void (*RefreshFunc)(void);
            RefreshFunc refresh = (RefreshFunc)dlsym(handle, "axe_preview_refresh");
            dispatch_sync(dispatch_get_main_queue(), ^{
                if (refresh) {
                    runbpRefresh = refresh;
                    refresh();
                    LOG("Called axe_preview_refresh");
                }
            });
            write(client, "OK\n", 3);
        } else {
            const char *err = dlerror();
            LOG("dlopen failed: %s", err ? err : "unknown");
            char resp[4096];
            snprintf(resp, sizeof(resp), "ERR:%s\n", err ? err : "unknown");
            write(client, resp, strlen(resp));
        }
        close(client);
    }
    return NULL;
}

__attribute__((constructor))
static void axe_loader_init(void) {
    const char *sock_path = getenv("AXE_PREVIEW_SOCKET_PATH");
    if (!sock_path || strlen(sock_path) == 0) {
        LOG("AXE_PREVIEW_SOCKET_PATH not set, loader inactive");
        return;
    }

    // Copy the path so it persists for the thread
    char *path_copy = strdup(sock_path);
    pthread_t tid;
    if (pthread_create(&tid, NULL, listener_thread, path_copy) != 0) {
        LOG("pthread_create failed");
        free(path_copy);
        return;
    }
    pthread_detach(tid);
    LOG("Loader initialized, socket: %s", sock_path);

    // Trigger initial preview refresh when the app becomes active.
    // Uses both notification observer and a delayed fallback to handle
    // cases where didBecomeActive fires before observer is registered.
    static BOOL didInitialRefresh = NO;

    void (^doInitialRefresh)(void) = ^{
        if (didInitialRefresh) return;
        didInitialRefresh = YES;
        typedef void (*RefreshFunc)(void);
        RefreshFunc refresh = (RefreshFunc)dlsym(RTLD_DEFAULT, "axe_preview_refresh");
        if (refresh) {
            runbpRefresh = refresh;
            refresh();
            dispatch_async(dispatch_get_main_queue(), ^{ runbpInitialReady = YES; });
            LOG("Initial preview refresh triggered");
        }
    };

    [[NSNotificationCenter defaultCenter]
        addObserverForName:UIApplicationDidBecomeActiveNotification
                    object:nil
                     queue:[NSOperationQueue mainQueue]
                usingBlock:^(NSNotification *note) {
        doInitialRefresh();
    }];

    // Fallback: if the notification was already posted before the observer
    // was registered (e.g. rapid relaunch), fire after a short delay.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.5 * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{
        doInitialRefresh();
    });
}
