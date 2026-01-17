from PIL import Image

# 定义需要的尺寸组合
sizes = [(16, 16), (32, 32), (48, 48), (180, 180), (256, 256)]

# 输入文件路径
input_files = [
    r"D:\Develop\Bealink\Server-win-Go+AHK\assets\dark_256px.png",
    r"D:\Develop\Bealink\Server-win-Go+AHK\assets\light_256px.png"
]

# 输出 ICO 文件路径
output_files = [
    r"D:\Develop\Bealink\Server-win-Go+AHK\assets\dark.ico",
    r"D:\Develop\Bealink\Server-win-Go+AHK\assets\light.ico"
]

# 转换过程
for in_file, out_file in zip(input_files, output_files):
    img = Image.open(in_file)
    img.save(out_file, format="ICO", sizes=sizes)

print("转换完成，已生成 ICO 文件。")
