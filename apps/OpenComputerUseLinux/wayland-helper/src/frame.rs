//! Content-Length framing, the same as the SDK protocol so both sides share one reader shape.

use std::io::{self, BufRead, Write};

const MAX_FRAME: usize = 64 << 20;

/// Reads one frame body. `Ok(None)` is a clean end of input between frames.
pub fn read(reader: &mut impl BufRead) -> io::Result<Option<Vec<u8>>> {
    let mut length = None;
    let mut header = 0;
    loop {
        let mut line = Vec::new();
        let read = reader.read_until(b'\n', &mut line)?;
        if read == 0 {
            return if header == 0 { Ok(None) } else { Err(invalid("truncated header")) };
        }
        header += read;
        if header > 8192 || !line.ends_with(b"\r\n") {
            return Err(invalid("invalid frame header"));
        }
        if line == b"\r\n" {
            break;
        }
        let text = std::str::from_utf8(&line[..line.len() - 2]).map_err(|_| invalid("invalid frame header"))?;
        let (key, value) = text.split_once(':').ok_or_else(|| invalid("invalid frame header"))?;
        if key.eq_ignore_ascii_case("Content-Length") {
            let value = value.trim();
            if length.is_some() || value.is_empty() || !value.bytes().all(|b| b.is_ascii_digit()) {
                return Err(invalid("invalid Content-Length"));
            }
            let parsed: usize = value.parse().map_err(|_| invalid("invalid Content-Length"))?;
            if parsed == 0 || parsed > MAX_FRAME {
                return Err(invalid("invalid Content-Length"));
            }
            length = Some(parsed);
        }
    }
    let mut body = vec![0; length.ok_or_else(|| invalid("missing Content-Length"))?];
    reader.read_exact(&mut body)?;
    Ok(Some(body))
}

pub fn write(writer: &mut impl Write, body: &[u8]) -> io::Result<()> {
    write!(writer, "Content-Length: {}\r\n\r\n", body.len())?;
    writer.write_all(body)?;
    writer.flush()
}

fn invalid(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_and_ends_cleanly() {
        let mut buffer = Vec::new();
        write(&mut buffer, br#"{"id":1}"#).unwrap();
        let mut reader = io::Cursor::new(buffer);
        assert_eq!(read(&mut reader).unwrap().unwrap(), br#"{"id":1}"#);
        assert!(read(&mut reader).unwrap().is_none());
    }

    #[test]
    fn rejects_malformed_headers() {
        for input in ["Content-Length: x\r\n\r\n", "Content-Length: 5\n\n", "\r\n", "Content-Length: 1\r\nContent-Length: 1\r\n\r\n", "Content-Length: 4\r\n"] {
            assert!(read(&mut io::Cursor::new(input.as_bytes())).is_err(), "{input:?}");
        }
    }

    #[test]
    fn rejects_truncated_body() {
        assert!(read(&mut io::Cursor::new(&b"Content-Length: 10\r\n\r\n{}"[..])).is_err());
    }
}
