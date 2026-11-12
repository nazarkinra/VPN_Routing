Программная отрисовка окна VPN routing

Окно программы рисуется через OpenGL 2.1. Если видеодрайвер его не даёт (нет драйвера видеокарты,
«Базовый видеоадаптер Microsoft», виртуальная машина, удалённый рабочий стол), окно рисуется процессором
с помощью этих библиотек. Включается само; вручную — переменная VPNROUTING_SOFTGL=1 или пустой файл softgl
в папке %APPDATA%\VPN routing.

opengl32.dll и libgallium_wgl.dll — Mesa 3D (драйвер llvmpipe), сборка mesa-dist-win:
  https://github.com/pal1000/mesa-dist-win
  https://mesa3d.org
Mesa распространяется по лицензии MIT (https://docs.mesa3d.org/license.html); входящий в libgallium_wgl.dll
LLVM — по лицензии Apache 2.0 with LLVM Exceptions (https://llvm.org/LICENSE.txt).
