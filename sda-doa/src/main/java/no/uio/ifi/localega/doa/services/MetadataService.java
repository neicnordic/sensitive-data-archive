package no.uio.ifi.localega.doa.services;

import lombok.extern.slf4j.Slf4j;
import no.uio.ifi.localega.doa.dto.File;
import no.uio.ifi.localega.doa.model.LEGADataset;
import no.uio.ifi.localega.doa.repositories.DatasetRepository;
import no.uio.ifi.localega.doa.repositories.FileRepository;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import java.util.Collection;
import java.util.Objects;
import java.util.Optional;
import java.util.Set;
import java.util.stream.Collectors;

/**
 * Service for accessing metadata (Files and Datasets).
 */
@Slf4j
@Service
public class MetadataService {

    @Autowired
    private FileRepository fileRepository;

    @Autowired
    private DatasetRepository datasetRepository;

    /**
     * Returns collection of dataset IDs present in the databse.
     *
     * @return Collection of dataset IDs.
     */
    public Collection<String> datasets(Set<String> datasetIds) {
        Collection<LEGADataset> datasets = datasetRepository.findByDatasetIdIn(datasetIds);
        return datasets.stream().filter(Objects::nonNull).map(LEGADataset::getDatasetId).collect(Collectors.toSet());
    }

    /**
     * Returns list of <code>File</code>'s by dataset ID.
     *
     * @param datasetId Dataset ID.
     * @return List of files in the dataset.
     */
    public Collection<File> files(String datasetId) {
        Collection<LEGADataset> datasets = datasetRepository.findByDatasetId(datasetId);
        return datasets
                .stream()
                .filter(Objects::nonNull)
                .map(LEGADataset::getFileId)
                .map(f -> fileRepository.findById(f))
                .flatMap(Optional::stream)
                .map(f -> {
                    File file = new File();
                    file.setFileId(f.getFileId());
                    file.setDatasetId(datasetId);
                    file.setDisplayFileName(f.getDisplayFileName());
                    file.setFileName(f.getFileName());
                    file.setFileSize(f.getFileSize());
                    file.setDecryptedFileSize(f.getDecryptedFileSize());
                    file.setDecryptedFileChecksum(f.getDecryptedFileChecksum());
                    file.setDecryptedFileChecksumType(f.getDecryptedFileChecksumType());
                    file.setUnencryptedChecksum(f.getUnencryptedChecksum());
                    file.setUnencryptedChecksumType(f.getUnencryptedChecksumType());
                    file.setFileStatus(f.getFileStatus());
                    return file;
                })
                .collect(Collectors.toSet());
    }

    /**
     * Returns file name by file ID.
     *
     * @return Filename.
     */
    public String getFileName(String fileId) {
        return fileRepository.findById(fileId).orElseThrow(RuntimeException::new).getDisplayFileName();
    }

}
