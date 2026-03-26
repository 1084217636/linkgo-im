package codec

import (
	// "bufio"
	"encoding/binary"
	// "fmt"
	"io"
)

// func Encode(data[]byte)([]byte,error) {
// 	// 1. 计算消息长度
// 	length := int32(len(data))

// 	// 2. 创建一个缓冲区 (4字节头 + 内容)
// 	pkg := make([]byte, 4+length)

// 	// 3. 写入头部 (使用大端序将 int32 转为 4个 byte)
// 	binary.BigEndian.PutUint32(pkg[:4], uint32(length))

// 	// 4. 写入内容
// 	copy(pkg[4:], data)

// 	return pkg, nil
// }
// // Decode 从连接中读取一个完整的包
// func Decode(reader io.Reader) ([]byte, error) {
// 	// 1. 先读前 4 个字节 (头部)
// 	lengthByte := make([]byte, 4)

// 	// 注意：这里必须用 ReadFull，保证一定读满4个字节
// 	_, err := io.ReadFull(reader, lengthByte)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// 2. 解析出内容长度
// 	length := binary.BigEndian.Uint32(lengthByte)

// 	// 3. 根据长度，读取剩下的内容
// 	data := make([]byte, length)
// 	_, err = io.ReadFull(reader, data)
// 	if err != nil {
// 		return nil, err
// 	}

//		return data, nil
//	}
func Encode(data []byte) ([]byte, error) {
	length := int32(len(data))

	pkg := make([]byte, 4+length)

	binary.BigEndian.PutUint32(pkg[:4], uint32(length)) //长度作为头4个

	copy(pkg[4:], data)

	return pkg, nil
}

func Decode(reader io.Reader) ([]byte, error) {
	lengthByte := make([]byte, 4)

	_, err := io.ReadFull(reader, lengthByte)
	if err != nil {
		return nil, err
	}
	//解析出长度
	length := binary.BigEndian.Uint32(lengthByte)
	data := make([]byte, length)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return nil, err
	}
	return data, nil

}
