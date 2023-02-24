package se.nbis.lega.inbox.sftp;

import net.schmizz.sshj.SSHClient;
import net.schmizz.sshj.sftp.SFTPClient;
import net.schmizz.sshj.transport.verification.PromiscuousVerifier;
import org.apache.commons.codec.digest.DigestUtils;
import org.apache.commons.io.FileUtils;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.test.context.junit4.SpringRunner;
import se.nbis.lega.inbox.pojo.EncryptedIntegrity;
import se.nbis.lega.inbox.pojo.FileDescriptor;

import java.io.File;
import java.io.IOException;
import java.nio.charset.Charset;
import java.util.concurrent.BlockingQueue;

import static org.apache.commons.codec.digest.MessageDigestAlgorithms.SHA_256;
import static org.junit.Assert.*;
import static se.nbis.lega.inbox.pojo.Operation.*;

@RunWith(SpringRunner.class)
public class InboxSftpEventListenerTest extends InboxTest {

    private int inboxPort;
    private BlockingQueue<FileDescriptor> fileBlockingQueue;
    private BlockingQueue<FileDescriptor> hashBlockingQueue;

    private File file;
    private File hash;
    private SSHClient ssh;
    private SFTPClient sftpClient;

    @Before
    public void setUp() throws IOException {
        File dataFolder = new File(System.getProperty("user.dir"));
        file = File.createTempFile("data", ".raw", dataFolder);

        file.deleteOnExit();
        FileUtils.writeStringToFile(file, "hello", Charset.defaultCharset());
        hash = File.createTempFile("data", ".sha256", dataFolder);
        hash.deleteOnExit();
        FileUtils.writeStringToFile(hash, "hello", Charset.defaultCharset());

        ssh = new SSHClient();
        ssh.addHostKeyVerifier(new PromiscuousVerifier());
        ssh.connect("localhost", inboxPort);
        ssh.authPassword(username, password);
        sftpClient = ssh.newSFTPClient();
    }

    @After
    public void tearDown() throws IOException {
        ssh.close();
    }

    @Test
    public void uploadFile() throws IOException {
        sftpClient.put(file.getAbsolutePath(), file.getName());

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        String expectedPath = username + "/" + file.getName();
        assertTrue(new File(expectedPath).exists());
        assertEquals(expectedPath, fileDescriptor.getFilePath());
        assertNull(fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        assertEquals(UPLOAD.name().toLowerCase(), fileDescriptor.getOperation());
        EncryptedIntegrity encryptedIntegrity = fileDescriptor.getEncryptedIntegrity()[0];
        assertNotNull(encryptedIntegrity);
        assertEquals(SHA_256.toLowerCase().replace("-", ""), encryptedIntegrity.getAlgorithm());
        assertEquals(DigestUtils.sha256Hex(FileUtils.openInputStream(file)), encryptedIntegrity.getChecksum());
    }

    @Test
    public void uploadHash() throws IOException {
        sftpClient.put(hash.getAbsolutePath(), hash.getName());

        FileDescriptor fileDescriptor = hashBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        String expectedPath = username + "/" + hash.getName();
        assertTrue(new File(expectedPath).exists());
        assertEquals(expectedPath, fileDescriptor.getFilePath());
        assertEquals(FileUtils.readFileToString(hash, Charset.defaultCharset()), fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        assertEquals(UPLOAD.name().toLowerCase(), fileDescriptor.getOperation());
        EncryptedIntegrity encryptedIntegrity = fileDescriptor.getEncryptedIntegrity()[0];
        assertNotNull(encryptedIntegrity);
        assertEquals(SHA_256.toLowerCase().replace("-", ""), encryptedIntegrity.getAlgorithm());
        assertEquals(DigestUtils.sha256Hex(FileUtils.openInputStream(file)), encryptedIntegrity.getChecksum());
    }

    @Test
    public void renameFile() throws IOException {
        sftpClient.put(file.getAbsolutePath(), file.getName());

        fileBlockingQueue.poll();
        sftpClient.mkdir("test");

        sftpClient.rename(file.getName(), "test/" + file.getName());

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        String expectedOldPath = username + "/" + file.getName();
        String expectedPath = username + "/test/" + file.getName();
        assertTrue(new File(expectedPath).exists());
        assertEquals(expectedPath, fileDescriptor.getFilePath());
        assertNull(fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        assertEquals(RENAME.name().toLowerCase(), fileDescriptor.getOperation());
        assertEquals(expectedOldPath, fileDescriptor.getOldPath());
        EncryptedIntegrity encryptedIntegrity = fileDescriptor.getEncryptedIntegrity()[0];
        assertNotNull(encryptedIntegrity);
        assertEquals(SHA_256.toLowerCase().replace("-", ""), encryptedIntegrity.getAlgorithm());
        assertEquals(DigestUtils.sha256Hex(FileUtils.openInputStream(file)), encryptedIntegrity.getChecksum());
    }

    @Test
    public void renameFolder() throws IOException {
        sftpClient.mkdir("test");
        sftpClient.mkdir("test/test1");

        sftpClient.put(file.getAbsolutePath(), "test/test1/" + file.getName());
        fileBlockingQueue.poll();

        sftpClient.rename("test/test1", "test/test2");

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        String expectedOldPath = username + "/test/test1";
        String expectedPath = username + "/test/test2";
        assertTrue(new File(expectedPath).exists());
        assertEquals(expectedPath, fileDescriptor.getFilePath());
        assertNull(fileDescriptor.getContent());
        assertEquals(0, fileDescriptor.getFileSize());
        assertEquals(RENAME.name().toLowerCase(), fileDescriptor.getOperation());
        assertEquals(expectedOldPath, fileDescriptor.getOldPath());
        Object encryptedIntegrity = fileDescriptor.getEncryptedIntegrity();
        assertNull(encryptedIntegrity);
    }

    @Test
    public void removeFile() throws IOException {
        sftpClient.put(file.getAbsolutePath(), file.getName());

        fileBlockingQueue.poll();
        sftpClient.rm(file.getName());

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        String expectedPath = username + "/" + file.getName();
        assertFalse(new File(expectedPath).exists());
        assertEquals(expectedPath, fileDescriptor.getFilePath());
        assertNull(fileDescriptor.getContent());
        assertEquals(0, fileDescriptor.getFileSize());
        assertEquals(REMOVE.name().toLowerCase(), fileDescriptor.getOperation());
        Object encryptedIntegrity = fileDescriptor.getEncryptedIntegrity();
        assertNull(encryptedIntegrity);
    }

    @Value("${inbox.port}")
    public void setInboxPort(int inboxPort) {
        this.inboxPort = inboxPort;
    }

    @Autowired
    public void setFileBlockingQueue(BlockingQueue<FileDescriptor> fileBlockingQueue) {
        this.fileBlockingQueue = fileBlockingQueue;
    }

    @Autowired
    public void setHashBlockingQueue(BlockingQueue<FileDescriptor> hashBlockingQueue) {
        this.hashBlockingQueue = hashBlockingQueue;
    }

}
