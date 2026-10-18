# Ancient AMI/Supermicro MegaRAC JViewer iKVM, wrapped in a modern browser console.
#
#   browser --(https/ws)--> kvm-web/websockify --> x11vnc --> Xvfb --> JViewer.jar (Java 8)
#                                                                        |
#                                                    AMI proprietary KVM (tcp 5901 + 5120/5123)
#                                                                        v
#                                                                       BMC
#
# JViewer.jar is signed with a Supermicro cert that expired years ago, so we invoke its
# Main-Class directly instead of going through Web Start -- plain `java -cp` does not
# validate JAR signatures, which sidesteps the entire trust-store problem.
FROM docker.io/library/eclipse-temurin:8-jre

ENV DEBIAN_FRONTEND=noninteractive

# xvfb/x11vnc/fluxbox    : the headless X stack JViewer's Swing UI draws into
# libxext6..libxi6       : Swing/AWT native deps, absent from the slim JRE image
# fontconfig+fonts-dejavu: without real fonts Swing renders blank boxes
# novnc/websockify       : browser-facing side
RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11vnc fluxbox \
        libxext6 libxrender1 libxtst6 libxi6 libxt6 \
        fontconfig fonts-dejavu-core \
        x11-utils x11-apps xauth \
        novnc websockify \
        python3 python3-requests \
        curl unzip ca-certificates procps net-tools \
    && rm -rf /var/lib/apt/lists/* \
    && rm -f /usr/share/novnc/index.html

# fluxbox calls fbsetbg on startup whatever the style says; with no wallpaper setter
# installed it answers by parking a 636x136 xmessage in the middle of the screen --
# i.e. on top of the console. This container paints the root window black via
# rootCommand and wants no wallpaper at all, so the nag has nothing to tell us.
RUN printf '#!/bin/sh\nexit 0\n' > /usr/bin/fbsetbg && chmod +x /usr/bin/fbsetbg

COPY fluxbox/ /root/.fluxbox/
COPY bin/ /usr/local/bin/
# The connect page takes over noVNC's index.html, so "/" is where you choose how to
# connect and the console stays where it has always been, at /vnc.html.
COPY web/ /usr/share/novnc/
# Keep the packaged noVNC UI and add our session controls to its console page.
RUN python3 -c 'from pathlib import Path; p = Path("/usr/share/novnc/vnc.html"); s = p.read_text(); assert "</head>" in s; p.write_text(s.replace("</head>", "<link rel=\"stylesheet\" href=\"/console-controls.css\">\n<script type=\"module\" src=\"/console-controls.js\"></script>\n</head>", 1))'
RUN chmod +x /usr/local/bin/*

ENV DISPLAY=:1 \
    SCREEN_GEOMETRY=1280x1024x24 \
    NOVNC_PORT=8080 \
    VNC_PORT=5999 \
    SESSION_PORT=8081

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
