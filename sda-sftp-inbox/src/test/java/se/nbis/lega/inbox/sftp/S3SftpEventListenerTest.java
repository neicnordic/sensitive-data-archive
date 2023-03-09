package se.nbis.lega.inbox.sftp;

import com.amazonaws.services.s3.AmazonS3;
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
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.TestPropertySource;
import org.springframework.test.context.junit4.SpringRunner;
import se.nbis.lega.inbox.pojo.EncryptedIntegrity;
import se.nbis.lega.inbox.pojo.FileDescriptor;
import se.nbis.lega.inbox.pojo.Operation;

import java.io.File;
import java.io.IOException;
import java.nio.charset.Charset;
import java.util.concurrent.BlockingQueue;

import static org.apache.commons.codec.digest.MessageDigestAlgorithms.SHA_256;
import static org.junit.Assert.*;

@SpringBootTest(classes = S3StorageInboxApplication.class)
@TestPropertySource(locations = "classpath:s3-storage.application.properties")
@RunWith(SpringRunner.class)
public class S3SftpEventListenerTest extends InboxTest {

    private int inboxPort;
    private BlockingQueue<FileDescriptor> fileBlockingQueue;
    private BlockingQueue<FileDescriptor> hashBlockingQueue;
    private AmazonS3 amazonS3;

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
        assertEquals(fileDescriptor.getUser() + "/" + file.getName(), fileDescriptor.getFilePath());
        assertTrue(amazonS3.doesObjectExist("default", fileDescriptor.getFilePath()));
        assertNull(fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        assertEquals(Operation.UPLOAD.name().toLowerCase(), fileDescriptor.getOperation());
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
        assertEquals(fileDescriptor.getUser() + "/" + hash.getName(), fileDescriptor.getFilePath());
        assertTrue(amazonS3.doesObjectExist("default", fileDescriptor.getFilePath()));
        assertEquals(FileUtils.readFileToString(hash, Charset.defaultCharset()), fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        assertEquals(Operation.UPLOAD.name().toLowerCase(), fileDescriptor.getOperation());
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
        assertEquals(fileDescriptor.getUser() + "/" + file.getName(), fileDescriptor.getOldPath());
        assertEquals(fileDescriptor.getUser() + "/test/" + file.getName(), fileDescriptor.getFilePath());
        assertFalse(amazonS3.doesObjectExist("default", fileDescriptor.getOldPath()));
        assertTrue(amazonS3.doesObjectExist("default", fileDescriptor.getFilePath()));
        assertNull(fileDescriptor.getContent());
        assertEquals(FileUtils.sizeOf(file), fileDescriptor.getFileSize());
        EncryptedIntegrity encryptedIntegrity = fileDescriptor.getEncryptedIntegrity()[0];
        assertNotNull(encryptedIntegrity);
        assertEquals(SHA_256.toLowerCase().replace("-", ""), encryptedIntegrity.getAlgorithm());
        assertEquals(DigestUtils.sha256Hex(FileUtils.openInputStream(file)), encryptedIntegrity.getChecksum());
    }

    @Test
    public void renameTopLevelFolder() throws IOException {
        sftpClient.mkdir("test");
        sftpClient.mkdir("test/test1");

        sftpClient.put(file.getAbsolutePath(), "test/test1/" + file.getName());
        fileBlockingQueue.poll();

        sftpClient.rename("test", "test2");

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        assertEquals(fileDescriptor.getUser() + "/test", fileDescriptor.getOldPath());
        assertEquals(fileDescriptor.getUser() + "/test2", fileDescriptor.getFilePath());
        assertFalse(amazonS3.doesObjectExist("default", fileDescriptor.getUser() + "/test/test1/" + file.getName()));
        assertTrue(amazonS3.doesObjectExist("default", fileDescriptor.getUser() + "/test2/test1/" + file.getName()));
        assertNull(fileDescriptor.getContent());
        assertEquals(0, fileDescriptor.getFileSize());
        Object encryptedIntegrity = fileDescriptor.getEncryptedIntegrity();
        assertNull(encryptedIntegrity);
    }

    @Test
    public void renameSecondLevelFolder() throws IOException {
        sftpClient.mkdir("test");
        sftpClient.mkdir("test/test1");

        sftpClient.put(file.getAbsolutePath(), "test/test1/" + file.getName());
        fileBlockingQueue.poll();

        sftpClient.rename("test/test1", "test/test2");

        FileDescriptor fileDescriptor = fileBlockingQueue.poll();
        assertNotNull(fileDescriptor);
        assertEquals(username, fileDescriptor.getUser());
        assertEquals(fileDescriptor.getUser() + "/test/test1", fileDescriptor.getOldPath());
        assertEquals(fileDescriptor.getUser() + "/test/test2", fileDescriptor.getFilePath());
        assertFalse(amazonS3.doesObjectExist("default", fileDescriptor.getOldPath() + "/" + file.getName()));
        assertTrue(amazonS3.doesObjectExist("default", fileDescriptor.getFilePath() + "/" + file.getName()));
        assertNull(fileDescriptor.getContent());
        assertEquals(0, fileDescriptor.getFileSize());
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
        String expectedPath = file.getName();
        assertFalse(amazonS3.doesObjectExist(fileDescriptor.getUser(), fileDescriptor.getFilePath()));
        assertEquals(fileDescriptor.getUser() + "/" + expectedPath, fileDescriptor.getFilePath());
        assertNull(fileDescriptor.getContent());
        assertEquals(0, fileDescriptor.getFileSize());
        assertEquals(Operation.REMOVE.name().toLowerCase(), fileDescriptor.getOperation());
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

    @Autowired
    public void setAmazonS3(AmazonS3 amazonS3) {
        this.amazonS3 = amazonS3;
    }

}
