package no.uio.ifi.localega.doa.services;

import io.minio.GetObjectArgs;
import io.minio.MinioClient;
import io.minio.errors.*;
import lombok.extern.slf4j.Slf4j;
import no.elixir.crypt4gh.pojo.header.DataEditList;
import no.elixir.crypt4gh.pojo.header.Header;
import no.elixir.crypt4gh.pojo.header.HeaderPacket;
import no.elixir.crypt4gh.pojo.header.X25519ChaCha20IETFPoly1305HeaderPacket;
import no.elixir.crypt4gh.stream.Crypt4GHInputStream;
import no.elixir.crypt4gh.util.Crypt4GHUtils;
import no.elixir.crypt4gh.util.KeyUtils;
import no.uio.ifi.localega.doa.dto.DestinationFormat;
import no.uio.ifi.localega.doa.model.LEGADataset;
import no.uio.ifi.localega.doa.model.LEGAFile;
import no.uio.ifi.localega.doa.repositories.DatasetRepository;
import no.uio.ifi.localega.doa.repositories.FileRepository;
import org.apache.commons.codec.binary.Hex;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import jakarta.security.auth.message.AuthException;
import java.io.*;
import java.math.BigInteger;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.*;
import java.util.Collection;

/**
 * Service for streaming files.
 */
@Slf4j
@Service
public class StreamingService {

    @Autowired
    private MinioClient archiveClient;

    @Autowired
    private FileRepository fileRepository;

    @Autowired
    private DatasetRepository datasetRepository;

    @Value("${s3.bucket}")
    private String s3Bucket;

    @Value("${crypt4gh.private-key-path}")
    private String crypt4ghPrivateKeyPath;

    @Value("${crypt4gh.private-key-password-path}")
    private String crypt4ghPrivateKeyPasswordPath;

    @Value("${archive.path}")
    private String archivePath;

    /**
     * Streams the requested file.
     *
     * @param datasetIds        IDs of datasets, available for this user.
     * @param publicKey         Optional public key, if the re-encryption was requested.
     * @param fileId            ID of the file to stream.
     * @param destinationFormat Destination format.
     * @param startCoordinate   Start byte.
     * @param endCoordinate     End byte.
     * @return File-stream.
     * @throws AuthException In case of access denied.
     * @throws Exception     In case of some other error.
     */
    public InputStream stream(Collection<String> datasetIds,
                              String publicKey,
                              String fileId,
                              String destinationFormat,
                              String startCoordinate,
                              String endCoordinate) throws AuthException, Exception {
        if (!checkPermissions(fileId, datasetIds)) {
            throw new AuthException("User doesn't have permissions to access requested file: " + fileId);
        }
        log.info("User has permissions to access requested file: {}", fileId);
        LEGAFile file = fileRepository.findById(fileId).orElseThrow(() -> new RuntimeException(String.format("File with ID %s doesn't exist", fileId)));
        byte[] header = Hex.decodeHex(file.getHeader());
        InputStream bodyInputStream = getFileInputStream(file);
        String password = Files.readString(Path.of(crypt4ghPrivateKeyPasswordPath));
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File(crypt4ghPrivateKeyPath), password.toCharArray());
        if (DestinationFormat.CRYPT4GH.name().equalsIgnoreCase(destinationFormat)) {
            return getEncryptedResponse(header, bodyInputStream, privateKey, startCoordinate, endCoordinate, publicKey);
        } else {
            return getPlaintextResponse(header, bodyInputStream, privateKey, startCoordinate, endCoordinate);
        }
    }

    private InputStream getPlaintextResponse(byte[] header, InputStream bodyInputStream, PrivateKey privateKey, String startCoordinate, String endCoordinate) throws IOException, GeneralSecurityException {
        ByteArrayInputStream headerInputStream = new ByteArrayInputStream(header);
        SequenceInputStream sequenceInputStream = new SequenceInputStream(headerInputStream, bodyInputStream);
        Crypt4GHInputStream crypt4GHInputStream;
        if (StringUtils.hasLength(startCoordinate) && StringUtils.hasLength(endCoordinate)) {
            DataEditList dataEditList = new DataEditList(new long[]{Long.parseLong(startCoordinate), Long.parseLong(endCoordinate)});
            crypt4GHInputStream = new Crypt4GHInputStream(sequenceInputStream, dataEditList, privateKey);
        } else {
            crypt4GHInputStream = new Crypt4GHInputStream(sequenceInputStream, privateKey);
        }
        return crypt4GHInputStream;
    }

    private InputStream getEncryptedResponse(byte[] header, InputStream bodyInputStream, PrivateKey privateKey, String startCoordinate, String endCoordinate, String publicKey) throws GeneralSecurityException, IOException {
        PublicKey recipientPublicKey = KeyUtils.getInstance().readPublicKey(publicKey);
        Header newHeader = Crypt4GHUtils.getInstance().setRecipient(header, privateKey, recipientPublicKey);
        if (StringUtils.hasLength(startCoordinate) && StringUtils.hasLength(endCoordinate)) {
            DataEditList dataEditList = new DataEditList(new long[]{Long.parseLong(startCoordinate), Long.parseLong(endCoordinate)});
            HeaderPacket dataEditListHeaderPacket = new X25519ChaCha20IETFPoly1305HeaderPacket(dataEditList, privateKey, recipientPublicKey);
            newHeader.getHeaderPackets().add(dataEditListHeaderPacket);
        }
        ByteArrayInputStream headerInputStream = new ByteArrayInputStream(newHeader.serialize());
        return new SequenceInputStream(headerInputStream, bodyInputStream);
    }

    private InputStream getFileInputStream(LEGAFile file) throws IOException, InvalidKeyException, NoSuchAlgorithmException, InsufficientDataException, InvalidResponseException, InternalException, ErrorResponseException, ServerException, XmlParserException {
        String filePath = file.getFilePath();
        try { // S3
            BigInteger s3FileId = new BigInteger(filePath);
            return archiveClient.getObject(GetObjectArgs.builder().bucket(s3Bucket).object(s3FileId.toString()).build());
        } catch (NumberFormatException e) { // filesystem
            String processedPath;
            if ("/".equalsIgnoreCase(archivePath)) {
                processedPath = filePath;
            } else {
                processedPath = archivePath + filePath;
            }
            processedPath = processedPath.replace("//", "/");
            log.info("Archive path is: {}", processedPath);
            return Files.newInputStream(new File(processedPath).toPath());
        }
    }

    private boolean checkPermissions(String fileId, Collection<String> datasetIds) {
        Collection<LEGADataset> datasets = datasetRepository.findByDatasetIdIn(datasetIds);
        for (LEGADataset dataset : datasets) {
            if (dataset.getFileId().equalsIgnoreCase(fileId)) {
                return true;
            }
        }
        return false;
    }

}
