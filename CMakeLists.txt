cmake_minimum_required(VERSION 3.22)
# The one version (VERSION): also embedded by the Go programs and read by the RPM build.
file(STRINGS ${CMAKE_CURRENT_SOURCE_DIR}/VERSION OLDIPMI_VERSION LIMIT_COUNT 1)
set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS VERSION)
project(OldIPMI VERSION ${OLDIPMI_VERSION} LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_AUTOMOC ON)
if(CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
  add_compile_options(-Wall -Wextra)
endif()
include(GNUInstallDirs)
option(BUILD_TESTING "Build the tests" ON)
find_package(Qt6 6.6 REQUIRED COMPONENTS Core Gui Widgets Quick Qml Network Test)
find_package(KF6Wallet REQUIRED)
find_package(KF6Kirigami REQUIRED)
# The app id is the name of the one desktop file, packaging/<app id>.desktop; the
# application, the RPM, the AppImage and tools/check.sh all take it from there.
file(GLOB OLDIPMI_DESKTOP_FILE CONFIGURE_DEPENDS ${CMAKE_SOURCE_DIR}/packaging/*.desktop)
list(LENGTH OLDIPMI_DESKTOP_FILE desktop_files)
if(NOT desktop_files EQUAL 1)
  message(FATAL_ERROR "packaging/ must hold exactly one .desktop file, named after the app id")
endif()
get_filename_component(OLDIPMI_APP_ID ${OLDIPMI_DESKTOP_FILE} NAME_WLE)
# The desktop code, compiled once for the application and its tests. STATIC on
# purpose: Fedora's %cmake turns BUILD_SHARED_LIBS on.
qt_add_library(oldipmi_desktop STATIC desktop/agentapi.cpp desktop/agentapi.h desktop/framethumbnail.cpp desktop/framethumbnail.h desktop/controller.cpp desktop/controller.h desktop/families.cpp desktop/families.h desktop/filedialogs.cpp desktop/filedialogs.h desktop/profilestore.cpp desktop/profilestore.h desktop/session.cpp desktop/session.h desktop/sessionmanager.cpp desktop/sessionmanager.h desktop/backendprocess.cpp desktop/backendprocess.h desktop/channels.cpp desktop/channels.h desktop/credentials.cpp desktop/credentials.h desktop/console.cpp desktop/console.h desktop/keymap.cpp desktop/keymap.h desktop/settings.cpp desktop/settings.h desktop/singleinstance.cpp desktop/singleinstance.h desktop/runtimedir.cpp desktop/runtimedir.h desktop/i18n.h desktop/ipc.h desktop/agentops.h)
target_link_libraries(oldipmi_desktop PUBLIC Qt6::Core Qt6::Gui Qt6::Widgets Qt6::Quick Qt6::Qml Qt6::Network KF6::Wallet)
# Code in subdirectories includes the desktop headers by name ("controller.h").
target_include_directories(oldipmi_desktop PUBLIC ${CMAKE_SOURCE_DIR}/desktop)
# The application: desktop/app starts it, desktop/devtools holds what only
# --self-test, its tour and --probe-ui run.
qt_add_executable(oldipmi desktop/main.cpp desktop/app/application.cpp desktop/app/application.h
                  desktop/app/exitcode.h desktop/app/options.cpp desktop/app/options.h
                  desktop/devtools/probeui.cpp desktop/devtools/probeui.h
                  desktop/devtools/qmlwarnings.cpp desktop/devtools/qmlwarnings.h
                  desktop/devtools/selftest.cpp desktop/devtools/selftest.h desktop/devtools/tour.cpp desktop/devtools/tour.h)
target_compile_definitions(oldipmi PRIVATE OLDIPMI_VERSION="${PROJECT_VERSION}" OLDIPMI_APP_ID="${OLDIPMI_APP_ID}")
# One instruction text for the GUI button and the MCP server's initialize reply.
configure_file(internal/mcp/instructions.md ${CMAKE_BINARY_DIR}/mcp_instructions.md COPYONLY)
file(GLOB QML_FILES CONFIGURE_DEPENDS RELATIVE ${CMAKE_SOURCE_DIR} desktop/qml/*.qml desktop/qml/*.js desktop/qml/qmldir)
qt_add_resources(oldipmi qml PREFIX "/" FILES ${QML_FILES})
qt_add_resources(oldipmi icon PREFIX "/" BASE packaging FILES packaging/oldipmi.svg)
file(GLOB IMAGE_FILES CONFIGURE_DEPENDS RELATIVE ${CMAKE_SOURCE_DIR} desktop/images/*.png)
qt_add_resources(oldipmi images PREFIX "/" BASE desktop FILES ${IMAGE_FILES})
# The console families (desktop/families.json): C++ and QML read the same table.
qt_add_resources(oldipmi families PREFIX "/" BASE desktop FILES desktop/families.json)
qt_add_resources(oldipmi mcp PREFIX "/" BASE ${CMAKE_BINARY_DIR} FILES ${CMAKE_BINARY_DIR}/mcp_instructions.md)
target_link_libraries(oldipmi PRIVATE oldipmi_desktop)
find_program(GO_EXECUTABLE go REQUIRED)
# What every Go program is built from. vendor/modules.txt exists only where the
# modules are vendored (the RPM build); it changes whenever they do.
file(GLOB_RECURSE GO_SOURCES CONFIGURE_DEPENDS internal/*.go cmd/*.go)
list(APPEND GO_SOURCES ${CMAKE_SOURCE_DIR}/version.go ${CMAKE_SOURCE_DIR}/VERSION ${CMAKE_SOURCE_DIR}/go.mod ${CMAKE_SOURCE_DIR}/go.sum)
if(EXISTS ${CMAKE_SOURCE_DIR}/vendor/modules.txt)
  list(APPEND GO_SOURCES ${CMAKE_SOURCE_DIR}/vendor/modules.txt)
endif()
# oldipmi_go_program(TARGET PROGRAM [DEPENDS files...]) builds ./cmd/PROGRAM into
# the build directory; DEPENDS names files the program embeds besides GO_SOURCES.
# "go build" inherits the environment (GOFLAGS in the RPM build).
function(oldipmi_go_program target program)
  cmake_parse_arguments(PARSE_ARGV 2 arg "" "" "DEPENDS")
  add_custom_command(OUTPUT ${CMAKE_BINARY_DIR}/${program}
    COMMAND ${CMAKE_COMMAND} -E env CGO_ENABLED=0 ${GO_EXECUTABLE} build -trimpath -o ${CMAKE_BINARY_DIR}/${program} ./cmd/${program}
    WORKING_DIRECTORY ${CMAKE_SOURCE_DIR} DEPENDS ${GO_SOURCES} ${arg_DEPENDS} VERBATIM)
  add_custom_target(${target} ALL DEPENDS ${CMAKE_BINARY_DIR}/${program})
endfunction()
oldipmi_go_program(core oldipmi-core)
oldipmi_go_program(mcp oldipmi-mcp DEPENDS ${CMAKE_SOURCE_DIR}/internal/mcp/instructions.md)
add_dependencies(oldipmi core mcp)
install(TARGETS oldipmi RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
install(PROGRAMS ${CMAKE_BINARY_DIR}/oldipmi-core ${CMAKE_BINARY_DIR}/oldipmi-mcp DESTINATION ${CMAKE_INSTALL_BINDIR})
install(FILES ${OLDIPMI_DESKTOP_FILE} DESTINATION ${CMAKE_INSTALL_DATADIR}/applications)
install(FILES packaging/oldipmi.svg DESTINATION ${CMAKE_INSTALL_DATADIR}/icons/hicolor/scalable/apps)
if(NOT BUILD_TESTING)
  return()
endif()
enable_testing()
# Every test runs with a fresh, private home and runtime directory (tools/isolated.sh).
set(ISOLATED ${CMAKE_SOURCE_DIR}/tools/isolated.sh)
add_test(NAME desktop_smoke COMMAND ${ISOLATED} $<TARGET_FILE:oldipmi> --self-test)
set_tests_properties(desktop_smoke PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen;QT_QUICK_BACKEND=software;QT_FORCE_STDERR_LOGGING=1")
# The screenshot tour: every UI state once; fails on a broken step or a QML warning.
add_test(NAME desktop_tour COMMAND ${ISOLATED} $<TARGET_FILE:oldipmi> --self-test --screenshots ${CMAKE_BINARY_DIR}/tour)
set_tests_properties(desktop_tour PROPERTIES TIMEOUT 300
                     ENVIRONMENT "QT_QPA_PLATFORM=offscreen;QT_QUICK_BACKEND=software;QT_FORCE_STDERR_LOGGING=1")
# --probe-ui: what it prints and its exit codes, against the demo console.
add_test(NAME probe_ui COMMAND ${ISOLATED} sh ${CMAKE_SOURCE_DIR}/desktop/probe_ui_test.sh $<TARGET_FILE:oldipmi>)
set_tests_properties(probe_ui PROPERTIES TIMEOUT 120
                     ENVIRONMENT "QT_QPA_PLATFORM=offscreen;QT_QUICK_BACKEND=software;QT_FORCE_STDERR_LOGGING=1")
qt_add_executable(profile_tests desktop/controller_test.cpp)
target_link_libraries(profile_tests PRIVATE oldipmi_desktop Qt6::Test)
target_compile_definitions(profile_tests PRIVATE IPC_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/ipc" FAKECORE_DIR="$<TARGET_FILE_DIR:fakecore>")
add_dependencies(profile_tests core fakecore)
add_test(NAME profiles COMMAND ${ISOLATED} $<TARGET_FILE:profile_tests>)
qt_add_executable(agent_tests desktop/agentapi_test.cpp)
qt_add_resources(agent_tests mcp PREFIX "/" BASE ${CMAKE_BINARY_DIR} FILES ${CMAKE_BINARY_DIR}/mcp_instructions.md)
target_link_libraries(agent_tests PRIVATE oldipmi_desktop Qt6::Test)
target_compile_definitions(agent_tests PRIVATE AGENT_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/agent" MCP_BINARY="${CMAKE_BINARY_DIR}/oldipmi-mcp"
                           FAKECORE_DIR="$<TARGET_FILE_DIR:fakecore>")
add_dependencies(agent_tests core mcp fakecore)
add_test(NAME agent_api COMMAND ${ISOLATED} $<TARGET_FILE:agent_tests>)
set_tests_properties(agent_api PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
set_tests_properties(profiles PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
qt_add_executable(console_tests desktop/console_test.cpp)
target_link_libraries(console_tests PRIVATE oldipmi_desktop Qt6::Test)
add_test(NAME console_input COMMAND ${ISOLATED} $<TARGET_FILE:console_tests>)
set_tests_properties(console_input PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen;QT_QUICK_BACKEND=software")
qt_add_executable(families_tests desktop/families_test.cpp)
target_compile_definitions(families_tests PRIVATE FAMILIES_JS="${CMAKE_SOURCE_DIR}/desktop/qml/families.js" IPC_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/ipc")
target_link_libraries(families_tests PRIVATE oldipmi_desktop Qt6::Test)
add_test(NAME error_hints COMMAND ${ISOLATED} $<TARGET_FILE:families_tests>)
# The tests whose code reads the family table (families.cpp).
foreach(target profile_tests agent_tests families_tests)
  qt_add_resources(${target} families PREFIX "/" BASE desktop FILES desktop/families.json)
endforeach()
qt_add_executable(singleinstance_tests desktop/singleinstance_test.cpp)
target_link_libraries(singleinstance_tests PRIVATE oldipmi_desktop Qt6::Test)
add_test(NAME single_instance COMMAND ${ISOLATED} $<TARGET_FILE:singleinstance_tests>)
# A stand-in for oldipmi-core that plays a script, for the IPC and Session tests.
qt_add_executable(fakecore desktop/fakecore_test.cpp)
target_link_libraries(fakecore PRIVATE Qt6::Core)
set_target_properties(fakecore PROPERTIES OUTPUT_NAME oldipmi-core RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/fakecore)
qt_add_executable(session_tests desktop/session_test.cpp)
target_compile_definitions(session_tests PRIVATE IPC_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/ipc" FAKECORE_DIR="$<TARGET_FILE_DIR:fakecore>")
target_link_libraries(session_tests PRIVATE oldipmi_desktop Qt6::Test)
add_dependencies(session_tests fakecore)
add_test(NAME session_ipc COMMAND ${ISOLATED} $<TARGET_FILE:session_tests>)
set_tests_properties(session_ipc PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
# The framing of the control and video sockets, without a core.
qt_add_executable(channels_tests desktop/channels_test.cpp)
target_link_libraries(channels_tests PRIVATE oldipmi_desktop Qt6::Test)
add_test(NAME ipc_channels COMMAND ${ISOLATED} $<TARGET_FILE:channels_tests>)
# The key table: the HID usage the BMC gets for each key.
qt_add_executable(keymap_tests desktop/keymap_test.cpp)
target_link_libraries(keymap_tests PRIVATE oldipmi_desktop Qt6::Test)
add_test(NAME key_table COMMAND ${ISOLATED} $<TARGET_FILE:keymap_tests>)
# desktop/ipc.h and agentops.h against testdata/{ipc,agent}/names.json, which the
# Go tests check too.
qt_add_executable(ipc_tests desktop/ipc_test.cpp desktop/ipc.h desktop/agentops.h)
target_compile_definitions(ipc_tests PRIVATE IPC_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/ipc" IPC_HEADER="${CMAKE_SOURCE_DIR}/desktop/ipc.h"
                           AGENT_FIXTURES="${CMAKE_SOURCE_DIR}/testdata/agent" AGENT_HEADER="${CMAKE_SOURCE_DIR}/desktop/agentops.h")
target_link_libraries(ipc_tests PRIVATE Qt6::Core Qt6::Test)
add_test(NAME ipc_names COMMAND ${ISOLATED} $<TARGET_FILE:ipc_tests>)
# Qt scans the QML imports of every program that links Qt Quick, to link static QML
# plugins. With a shared Qt it links nothing and only warns about Kirigami's plugins,
# so the tests skip it, as Qt's own tests do.
set_target_properties(profile_tests agent_tests console_tests families_tests singleinstance_tests session_tests channels_tests
                      keymap_tests PROPERTIES QT_QML_MODULE_NO_IMPORT_SCAN TRUE)
# Labels: "ui" starts the application offscreen, "unit" is the rest. The time
# limits leave room for the sanitizer build in tools/check.sh (about 3x slower).
set_tests_properties(desktop_smoke desktop_tour probe_ui PROPERTIES LABELS ui)
set_tests_properties(desktop_smoke PROPERTIES TIMEOUT 120)
set_tests_properties(profiles agent_api console_input error_hints single_instance session_ipc ipc_channels key_table
                     ipc_names PROPERTIES LABELS unit TIMEOUT 120)
